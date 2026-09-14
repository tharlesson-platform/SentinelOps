// kubeinventory creates a bounded, read-only Kubernetes inventory snapshot.
// It uses kubectl only as a transport so the same binary works with a projected
// ServiceAccount token or an approved operator context.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxAssets = 1000

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

type config struct {
	Context     string   `json:"context"`
	Namespaces  []string `json:"namespaces"`
	Environment string   `json:"environment"`
	OwnerTeam   string   `json:"ownerTeam"`
}

type metadata struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type object struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   metadata `json:"metadata"`
}

type objectList struct {
	Items []object `json:"items"`
}

type asset struct {
	AssetID     string            `json:"assetId"`
	Name        string            `json:"name"`
	Kind        string            `json:"kind"`
	Site        string            `json:"site"`
	OwnerTeam   string            `json:"ownerTeam"`
	Environment string            `json:"environment"`
	Lifecycle   string            `json:"lifecycle"`
	Labels      map[string]string `json:"labels"`
}

type snapshot struct {
	ObservedAt time.Time `json:"observedAt"`
	Complete   bool      `json:"complete"`
	Assets     []asset   `json:"assets"`
}

type runner func(context.Context, string, ...string) ([]byte, error)

func main() {
	configPath := flag.String("config", "", "arquivo JSON com contexto e namespaces autorizados")
	outputPath := flag.String("output", "", "snapshot de saída")
	flag.Parse()
	if *configPath == "" || *outputPath == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "uso: kubeinventory --config SCOPE.json --output INVENTORY.json")
		os.Exit(2)
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro de configuração:", err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := collect(ctx, cfg, command, time.Now().UTC())
	if err != nil {
		fmt.Fprintln(os.Stderr, "coleta Kubernetes não publicada:", err)
		os.Exit(1)
	}
	if err := writeSnapshot(*outputPath, result); err != nil {
		fmt.Fprintln(os.Stderr, "erro ao gravar snapshot:", err)
		os.Exit(1)
	}
	fmt.Printf("snapshot Kubernetes publicado: context=%s assets=%d observed_at=%s\n", cfg.Context, len(result.Assets), result.ObservedAt.Format(time.RFC3339))
}

func loadConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var cfg config
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return cfg, errors.New("arquivo contém mais de um documento JSON")
	}
	cfg.Context, cfg.Environment, cfg.OwnerTeam = strings.TrimSpace(cfg.Context), strings.TrimSpace(cfg.Environment), strings.TrimSpace(cfg.OwnerTeam)
	if !safeName.MatchString(cfg.Context) || !safeName.MatchString(cfg.Environment) || !safeName.MatchString(cfg.OwnerTeam) {
		return cfg, errors.New("context, environment e ownerTeam devem ser DNS-safe")
	}
	if len(cfg.Namespaces) == 0 || len(cfg.Namespaces) > 100 {
		return cfg, errors.New("namespaces requer entre 1 e 100 entradas explícitas")
	}
	seen := map[string]bool{}
	for i := range cfg.Namespaces {
		cfg.Namespaces[i] = strings.TrimSpace(cfg.Namespaces[i])
		if !safeName.MatchString(cfg.Namespaces[i]) || seen[cfg.Namespaces[i]] {
			return cfg, errors.New("cada namespace deve ser DNS-safe e único")
		}
		seen[cfg.Namespaces[i]] = true
	}
	sort.Strings(cfg.Namespaces)
	return cfg, nil
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	child, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(child, name, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %s", name, classify(output, child.Err()))
	}
	return output, nil
}

func classify(output []byte, contextErr error) string {
	if contextErr != nil {
		return "command_timeout"
	}
	message := strings.ToLower(string(output))
	switch {
	case strings.Contains(message, "forbidden"), strings.Contains(message, "unauthorized"):
		return "access_denied"
	case strings.Contains(message, "not found"):
		return "not_found"
	case strings.Contains(message, "timeout"), strings.Contains(message, "connection refused"), strings.Contains(message, "unavailable"):
		return "transient_failure"
	default:
		return "command_failed"
	}
}

func collect(ctx context.Context, cfg config, run runner, observedAt time.Time) (snapshot, error) {
	base := []string{"--context", cfg.Context}
	clusterID, err := clusterIdentity(ctx, run, base)
	if err != nil {
		return snapshot{}, err
	}
	if err := verifyReadOnly(ctx, cfg, run, base); err != nil {
		return snapshot{}, err
	}
	assets, err := collectObjects(ctx, cfg, run, base, clusterID)
	if err != nil {
		return snapshot{}, err
	}
	if len(assets) > maxAssets {
		return snapshot{}, fmt.Errorf("a coleta retornou %d ativos; limite por agente é %d, divida namespaces", len(assets), maxAssets)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].AssetID < assets[j].AssetID })
	return snapshot{ObservedAt: observedAt.UTC(), Complete: true, Assets: assets}, nil
}

func clusterIdentity(ctx context.Context, run runner, base []string) (string, error) {
	args := append(append([]string{}, base...), "get", "namespace", "kube-system", "-o", "json")
	data, err := run(ctx, "kubectl", args...)
	if err != nil {
		return "", fmt.Errorf("identidade do cluster: %w", err)
	}
	var item object
	if err := json.Unmarshal(data, &item); err != nil || item.Metadata.UID == "" || item.Metadata.Name != "kube-system" {
		return "", errors.New("namespace kube-system não retornou UID estável")
	}
	return item.Metadata.UID, nil
}

func verifyReadOnly(ctx context.Context, cfg config, run runner, base []string) error {
	for _, namespace := range cfg.Namespaces {
		for _, resource := range []string{"pods", "services", "deployments", "statefulsets", "daemonsets", "jobs", "cronjobs", "persistentvolumeclaims"} {
			if err := canI(ctx, run, base, "list", resource, namespace, true); err != nil {
				return err
			}
		}
		if err := canI(ctx, run, base, "get", "secrets", namespace, false); err != nil {
			return err
		}
		if err := canI(ctx, run, base, "create", "deployments", namespace, false); err != nil {
			return err
		}
	}
	return canI(ctx, run, base, "list", "nodes", "", true)
}

func canI(ctx context.Context, run runner, base []string, verb, resource, namespace string, expected bool) error {
	args := append(append([]string{}, base...), "auth", "can-i", verb, resource)
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}
	data, err := run(ctx, "kubectl", args...)
	if err != nil {
		return fmt.Errorf("verificação RBAC %s %s: %w", verb, resource, err)
	}
	actual := strings.TrimSpace(string(data)) == "yes"
	if actual != expected {
		return fmt.Errorf("RBAC inesperado para %s %s no namespace %s", verb, resource, namespace)
	}
	return nil
}

func collectObjects(ctx context.Context, cfg config, run runner, base []string, clusterID string) ([]asset, error) {
	assets := []asset{}
	seen := map[string]bool{}
	collectList := func(args ...string) error {
		data, err := run(ctx, "kubectl", append(base, args...)...)
		if err != nil {
			return err
		}
		var list objectList
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		for _, item := range list.Items {
			if item.Metadata.UID == "" || item.Metadata.Name == "" || item.Kind == "" {
				return errors.New("Kubernetes retornou objeto sem UID, nome ou kind")
			}
			identity := clusterID + ":" + item.APIVersion + ":" + item.Kind + ":" + item.Metadata.UID
			if seen[identity] {
				return errors.New("Kubernetes retornou objeto duplicado")
			}
			seen[identity] = true
			name := item.Metadata.Name
			if item.Metadata.Namespace != "" {
				name = item.Metadata.Namespace + "/" + name
			}
			digest := sha256.Sum256([]byte(identity))
			assets = append(assets, asset{AssetID: "k8s:" + hex.EncodeToString(digest[:]), Name: name, Kind: "kubernetes/" + strings.ToLower(item.Kind), Site: cfg.Context, OwnerTeam: cfg.OwnerTeam, Environment: cfg.Environment, Lifecycle: "active", Labels: map[string]string{"kubernetes_cluster_uid": clusterID, "kubernetes_context": cfg.Context, "kubernetes_kind": item.Kind, "kubernetes_uid": item.Metadata.UID, "kubernetes_namespace": item.Metadata.Namespace}})
		}
		return nil
	}
	if err := collectList("get", "nodes", "-o", "json"); err != nil {
		return nil, fmt.Errorf("lista nodes: %w", err)
	}
	for _, namespace := range cfg.Namespaces {
		if err := collectList("get", "pods,services,deployments,statefulsets,daemonsets,jobs,cronjobs,persistentvolumeclaims", "--namespace", namespace, "-o", "json"); err != nil {
			return nil, fmt.Errorf("lista de recursos em %s: %w", namespace, err)
		}
	}
	return assets, nil
}

func writeSnapshot(path string, data snapshot) error {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".kubeinventory-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
