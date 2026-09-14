// cloudinventory collects a bounded, read-only cloud inventory through the
// locally authenticated official CLI and writes an atomic SentinelOps snapshot.
// It intentionally supports one approved scope per invocation: the receiving
// agent's identity is the inventory source and prevents cross-scope staleness.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxAssets = 1000

var (
	uuidPattern    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	accountPattern = regexp.MustCompile(`^[0-9]{12}$`)
	regionPattern  = regexp.MustCompile(`^[a-z]{2}(-gov)?-[a-z0-9-]+-[0-9]+$`)
	profilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
)

type scopeFile struct {
	Provider    string     `json:"provider"`
	Environment string     `json:"environment"`
	OwnerTeam   string     `json:"ownerTeam"`
	Scope       cloudScope `json:"scope"`
}

type cloudScope struct {
	SubscriptionID string `json:"subscriptionId,omitempty"`
	AccountID      string `json:"accountId,omitempty"`
	Region         string `json:"region,omitempty"`
	Profile        string `json:"profile,omitempty"`
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

type commandRunner func(context.Context, string, ...string) ([]byte, error)

func shell(ctx context.Context, name string, args ...string) ([]byte, error) {
	commandContext, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %s", name, classifyCommandFailure(err, output))
	}
	return output, nil
}

// classifyCommandFailure deliberately does not return raw CLI output: cloud
// error payloads can contain request metadata that does not belong in logs.
func classifyCommandFailure(err error, output []byte) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "command_timeout"
	}
	message := strings.ToLower(string(output))
	switch {
	case strings.Contains(message, "429"), strings.Contains(message, "throttl"), strings.Contains(message, "too many requests"), strings.Contains(message, "rate exceeded"):
		return "rate_limited"
	case strings.Contains(message, "expired"), strings.Contains(message, "token has expired"), strings.Contains(message, "aadsts"):
		return "credential_expired"
	case strings.Contains(message, "accessdenied"), strings.Contains(message, "authorizationfailed"), strings.Contains(message, "forbidden"), strings.Contains(message, "unauthorized"):
		return "access_denied"
	case strings.Contains(message, "timeout"), strings.Contains(message, "temporarily unavailable"), strings.Contains(message, "connection reset"), strings.Contains(message, "service unavailable"), strings.Contains(message, " 500"), strings.Contains(message, " 502"), strings.Contains(message, " 503"):
		return "transient_failure"
	default:
		return "command_failed"
	}
}

func retrying(run commandRunner) commandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		var err error
		for attempt := 0; attempt < 4; attempt++ {
			var result []byte
			result, err = run(ctx, name, args...)
			if err == nil {
				return result, nil
			}
			if !retryable(err) || attempt == 3 {
				return nil, err
			}
			delay := backoffWithJitter(attempt)
			fmt.Fprintf(os.Stderr, "cloud inventory retry: command=%s attempt=%d delay=%s reason=%s\n", name, attempt+1, delay, retryReason(err))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		return nil, err
	}
}

func retryable(err error) bool {
	reason := retryReason(err)
	return reason == "rate_limited" || reason == "transient_failure" || reason == "command_timeout"
}

func retryReason(err error) string {
	message := err.Error()
	for _, reason := range []string{"rate_limited", "transient_failure", "command_timeout", "credential_expired", "access_denied", "command_failed"} {
		if strings.Contains(message, reason) {
			return reason
		}
	}
	return "command_failed"
}

func backoffWithJitter(attempt int) time.Duration {
	base := 250 * time.Millisecond * time.Duration(1<<attempt)
	// Randomize between 50% and 100% of the exponential interval to avoid a
	// synchronized retry storm from collectors in the same site.
	spread, err := rand.Int(rand.Reader, big.NewInt(int64(base/2)+1))
	if err != nil {
		return base
	}
	return base/2 + time.Duration(spread.Int64())
}

func main() {
	configPath := flag.String("config", "", "arquivo JSON de escopo cloud aprovado")
	outputPath := flag.String("output", "", "snapshot de inventário de saída")
	flag.Parse()
	if *configPath == "" || *outputPath == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "uso: cloudinventory --config SCOPE.json --output INVENTORY.json")
		os.Exit(2)
	}
	cfg, err := loadScope(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro de configuração:", err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := collect(ctx, cfg, shell, time.Now().UTC())
	if err != nil {
		fmt.Fprintln(os.Stderr, "coleta cloud não publicada:", err)
		os.Exit(1)
	}
	if err := writeSnapshot(*outputPath, result); err != nil {
		fmt.Fprintln(os.Stderr, "erro ao gravar snapshot:", err)
		os.Exit(1)
	}
	fmt.Printf("snapshot cloud publicado: provider=%s assets=%d observed_at=%s\n", cfg.Provider, len(result.Assets), result.ObservedAt.Format(time.RFC3339))
}

func loadScope(path string) (scopeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return scopeFile{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var cfg scopeFile
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return cfg, errors.New("arquivo contém mais de um documento JSON")
	}
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	cfg.Environment = strings.TrimSpace(cfg.Environment)
	cfg.OwnerTeam = strings.TrimSpace(cfg.OwnerTeam)
	if cfg.Provider != "azure" && cfg.Provider != "aws" {
		return cfg, errors.New("provider deve ser azure ou aws")
	}
	if cfg.Environment == "" || cfg.OwnerTeam == "" {
		return cfg, errors.New("environment e ownerTeam são obrigatórios")
	}
	if cfg.Provider == "azure" {
		if !uuidPattern.MatchString(cfg.Scope.SubscriptionID) || cfg.Scope.AccountID != "" || cfg.Scope.Region != "" || cfg.Scope.Profile != "" {
			return cfg, errors.New("Azure requer apenas scope.subscriptionId UUID")
		}
		return cfg, nil
	}
	if !accountPattern.MatchString(cfg.Scope.AccountID) || !regionPattern.MatchString(cfg.Scope.Region) || !profilePattern.MatchString(cfg.Scope.Profile) || cfg.Scope.SubscriptionID != "" {
		return cfg, errors.New("AWS requer somente scope.accountId (12 dígitos), region e profile seguro")
	}
	return cfg, nil
}

func collect(ctx context.Context, cfg scopeFile, run commandRunner, observedAt time.Time) (snapshot, error) {
	run = retrying(run)
	var assets []asset
	var err error
	switch cfg.Provider {
	case "azure":
		assets, err = collectAzure(ctx, cfg, run)
	case "aws":
		assets, err = collectAWS(ctx, cfg, run)
	}
	if err != nil {
		return snapshot{}, err
	}
	if len(assets) > maxAssets {
		return snapshot{}, fmt.Errorf("a coleta retornou %d ativos; limite por identidade de agente é %d, divida o escopo", len(assets), maxAssets)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].AssetID < assets[j].AssetID })
	return snapshot{ObservedAt: observedAt.UTC(), Complete: true, Assets: assets}, nil
}

func collectAzure(ctx context.Context, cfg scopeFile, run commandRunner) ([]asset, error) {
	identity, err := run(ctx, "az", "account", "show", "--subscription", cfg.Scope.SubscriptionID, "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("validação da subscription Azure: %w", err)
	}
	var account struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(identity, &account); err != nil || !strings.EqualFold(account.ID, cfg.Scope.SubscriptionID) {
		return nil, errors.New("a identidade Azure autenticada não corresponde à subscription autorizada")
	}
	const query = "Resources | project id, name, type, location, resourceGroup, subscriptionId | order by id asc"
	token := ""
	seen := map[string]bool{}
	assets := []asset{}
	for pages := 0; ; pages++ {
		if pages >= 100 {
			return nil, errors.New("paginação Azure excedeu 100 páginas")
		}
		args := []string{"graph", "query", "--subscriptions", cfg.Scope.SubscriptionID, "--first", "1000", "--query", query, "--output", "json"}
		if token != "" {
			args = append(args, "--skip-token", token)
		}
		pageData, err := run(ctx, "az", args...)
		if err != nil {
			return nil, fmt.Errorf("consulta Azure Resource Graph: %w", err)
		}
		var page struct {
			Data []struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				Type           string `json:"type"`
				Location       string `json:"location"`
				ResourceGroup  string `json:"resourceGroup"`
				SubscriptionID string `json:"subscriptionId"`
			} `json:"data"`
			SkipToken string `json:"skipToken"`
		}
		if err := json.Unmarshal(pageData, &page); err != nil {
			return nil, fmt.Errorf("decodifica página Azure: %w", err)
		}
		for _, item := range page.Data {
			if item.ID == "" || item.Name == "" || item.Type == "" || !strings.EqualFold(item.SubscriptionID, cfg.Scope.SubscriptionID) {
				return nil, errors.New("Resource Graph retornou recurso incompleto ou fora da subscription autorizada")
			}
			if seen[item.ID] {
				return nil, errors.New("Resource Graph retornou resource ID duplicado")
			}
			seen[item.ID] = true
			assets = append(assets, makeAsset("azure", item.ID, item.Name, item.Type, item.Location, cfg, map[string]string{
				"azure_subscription_id": strings.ToLower(item.SubscriptionID), "azure_resource_group": item.ResourceGroup, "azure_resource_id": item.ID,
			}))
		}
		if page.SkipToken == "" {
			return assets, nil
		}
		token = page.SkipToken
	}
}

func collectAWS(ctx context.Context, cfg scopeFile, run commandRunner) ([]asset, error) {
	base := []string{"--no-cli-pager", "--profile", cfg.Scope.Profile, "--region", cfg.Scope.Region}
	identity, err := run(ctx, "aws", append(base, "sts", "get-caller-identity", "--output", "json")...)
	if err != nil {
		return nil, fmt.Errorf("validação da conta AWS: %w", err)
	}
	var caller struct {
		Account string `json:"Account"`
	}
	if err := json.Unmarshal(identity, &caller); err != nil || caller.Account != cfg.Scope.AccountID {
		return nil, errors.New("a identidade AWS autenticada não corresponde à account autorizada")
	}
	if err := validateAWSConfig(ctx, run, base); err != nil {
		return nil, err
	}
	const expression = "SELECT resourceId, resourceType, resourceName, awsRegion, accountId, configurationItemCaptureTime"
	token := ""
	seen := map[string]bool{}
	assets := []asset{}
	for pages := 0; ; pages++ {
		if pages >= 100 {
			return nil, errors.New("paginação AWS Config excedeu 100 páginas")
		}
		args := append([]string{}, base...)
		args = append(args, "configservice", "select-resource-config", "--expression", expression, "--limit", "100", "--output", "json")
		if token != "" {
			args = append(args, "--next-token", token)
		}
		pageData, err := run(ctx, "aws", args...)
		if err != nil {
			return nil, fmt.Errorf("consulta AWS Config read-only: %w", err)
		}
		var page struct {
			Results   []string `json:"Results"`
			NextToken string   `json:"NextToken"`
		}
		if err := json.Unmarshal(pageData, &page); err != nil {
			return nil, fmt.Errorf("decodifica página AWS Config: %w", err)
		}
		for _, raw := range page.Results {
			var item struct {
				ResourceID   string `json:"resourceId"`
				ResourceType string `json:"resourceType"`
				ResourceName string `json:"resourceName"`
				Region       string `json:"awsRegion"`
				AccountID    string `json:"accountId"`
			}
			if err := json.Unmarshal([]byte(raw), &item); err != nil {
				return nil, fmt.Errorf("decodifica recurso AWS Config: %w", err)
			}
			if item.ResourceID == "" || item.ResourceType == "" || item.AccountID != cfg.Scope.AccountID || item.Region != cfg.Scope.Region {
				return nil, errors.New("AWS Config retornou recurso incompleto ou fora da account/região autorizada")
			}
			identity := item.AccountID + ":" + item.Region + ":" + item.ResourceType + ":" + item.ResourceID
			if seen[identity] {
				return nil, errors.New("AWS Config retornou recurso duplicado")
			}
			seen[identity] = true
			name := item.ResourceName
			if name == "" {
				name = item.ResourceID
			}
			assets = append(assets, makeAsset("aws", identity, name, item.ResourceType, item.Region, cfg, map[string]string{
				"aws_account_id": item.AccountID, "aws_region": item.Region, "aws_resource_id": item.ResourceID,
			}))
		}
		if page.NextToken == "" {
			return assets, nil
		}
		token = page.NextToken
	}
}

func validateAWSConfig(ctx context.Context, run commandRunner, base []string) error {
	args := append(append([]string{}, base...), "configservice", "describe-configuration-recorders", "--output", "json")
	recorders, err := run(ctx, "aws", args...)
	if err != nil {
		return fmt.Errorf("verificação AWS Config: %w", err)
	}
	var recorderResponse struct {
		ConfigurationRecorders []struct {
			RecordingGroup struct {
				AllSupported bool `json:"allSupported"`
			} `json:"recordingGroup"`
		} `json:"ConfigurationRecorders"`
	}
	if err := json.Unmarshal(recorders, &recorderResponse); err != nil {
		return fmt.Errorf("decodifica recorder AWS Config: %w", err)
	}
	allSupported := false
	for _, recorder := range recorderResponse.ConfigurationRecorders {
		allSupported = allSupported || recorder.RecordingGroup.AllSupported
	}
	if !allSupported {
		return errors.New("AWS Config não está configurado para registrar todos os tipos suportados; snapshot completo recusado")
	}
	args = append(append([]string{}, base...), "configservice", "describe-configuration-recorder-status", "--output", "json")
	statuses, err := run(ctx, "aws", args...)
	if err != nil {
		return fmt.Errorf("status AWS Config: %w", err)
	}
	var statusResponse struct {
		ConfigurationRecordersStatus []struct {
			Recording bool `json:"recording"`
		} `json:"ConfigurationRecordersStatus"`
	}
	if err := json.Unmarshal(statuses, &statusResponse); err != nil {
		return fmt.Errorf("decodifica status AWS Config: %w", err)
	}
	for _, status := range statusResponse.ConfigurationRecordersStatus {
		if status.Recording {
			return nil
		}
	}
	return errors.New("AWS Config não está gravando; snapshot completo recusado")
}

func makeAsset(provider, stableID, name, kind, site string, cfg scopeFile, labels map[string]string) asset {
	digest := sha256.Sum256([]byte(stableID))
	labels["cloud_provider"] = provider
	return asset{AssetID: provider + ":" + hex.EncodeToString(digest[:]), Name: name, Kind: kind, Site: site, OwnerTeam: cfg.OwnerTeam, Environment: cfg.Environment, Lifecycle: "active", Labels: labels}
}

func writeSnapshot(path string, data snapshot) error {
	if path == "" || filepath.Base(path) == "." {
		return errors.New("caminho de saída inválido")
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".cloudinventory-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
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
	return os.Rename(temporaryName, path)
}
