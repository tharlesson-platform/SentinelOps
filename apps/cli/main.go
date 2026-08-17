package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sentinelops/sentinelops/internal/apiclient"
	"gopkg.in/yaml.v3"
)

const version = "0.1.0"

type configFile struct {
	Current  string                `yaml:"current"`
	Contexts map[string]cliContext `yaml:"contexts"`
}
type cliContext struct {
	URL      string `yaml:"url"`
	Token    string `yaml:"token,omitempty"`
	MTLSCert string `yaml:"mtlsCert,omitempty"`
	MTLSKey  string `yaml:"mtlsKey,omitempty"`
}
type globals struct {
	output  string
	timeout time.Duration
	noColor bool
	quiet   bool
}

func main() { code := run(os.Args[1:]); os.Exit(code) }
func run(args []string) int {
	g, args, err := parseGlobals(args)
	if err != nil {
		return exitErr(g, 3, err)
	}
	if len(args) == 0 {
		usage()
		return 3
	}
	cfgPath := configPath()
	cfg, _ := loadConfig(cfgPath)
	ctxCfg := cfg.Contexts[cfg.Current]
	if u := os.Getenv("SENTINEL_API_URL"); u != "" {
		ctxCfg.URL = u
	}
	if t := os.Getenv("SENTINEL_TOKEN"); t != "" {
		ctxCfg.Token = t
	}
	if ctxCfg.URL == "" {
		ctxCfg.URL = "http://localhost:8080"
	}
	client := apiclient.New(ctxCfg.URL, ctxCfg.Token, g.timeout)
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	defer cancel()
	switch strings.Join(args[:min(2, len(args))], " ") {
	case "version":
		return printOut(g, map[string]string{"version": version})
	case "login":
		return cmdLogin(ctx, g, client, cfgPath, &cfg, args[1:])
	case "logout":
		ctxCfg.Token = ""
		cfg.Contexts[cfg.Current] = ctxCfg
		if err := saveConfig(cfgPath, cfg); err != nil {
			return exitErr(g, 3, err)
		}
		return printOut(g, map[string]string{"status": "logged out"})
	case "doctor":
		return cmdDoctor(ctx, g, client)
	case "context list":
		return printOut(g, cfg)
	case "context use":
		if len(args) < 3 {
			return exitErr(g, 3, errors.New("context use requer um nome"))
		}
		if _, ok := cfg.Contexts[args[2]]; !ok {
			return exitErr(g, 3, errors.New("contexto não encontrado"))
		}
		cfg.Current = args[2]
		if err := saveConfig(cfgPath, cfg); err != nil {
			return exitErr(g, 3, err)
		}
		return printOut(g, map[string]string{"current": cfg.Current})
	case "service list":
		return get(ctx, g, client, "/api/v1/services")
	case "service get":
		if len(args) < 3 {
			return exitErr(g, 3, errors.New("service get requer nome"))
		}
		return get(ctx, g, client, "/api/v1/services/"+args[2])
	case "service apply":
		return applyFile(ctx, g, client, "/api/v1/services", args[2:])
	case "service delete":
		if len(args) < 3 {
			return exitErr(g, 3, errors.New("service delete requer nome"))
		}
		return call(ctx, g, client, "DELETE", "/api/v1/services/"+args[2], nil, nil)
	case "scenario list":
		return get(ctx, g, client, "/api/v1/scenarios")
	case "scenario validate", "config validate":
		return validateFile(g, args[2:])
	case "scenario apply":
		return applyFile(ctx, g, client, "/api/v1/scenarios", args[2:])
	case "release register":
		return cmdRelease(ctx, g, client, args[2:])
	case "release validate":
		return cmdReleaseValidate(ctx, g, client, args[2:])
	case "validation get":
		if len(args) < 3 {
			return exitErr(g, 3, errors.New("validation get requer id"))
		}
		return get(ctx, g, client, "/api/v1/validations/"+args[2])
	case "validation wait":
		return cmdWait(ctx, g, client, args[2:])
	case "agent list", "agent status":
		return get(ctx, g, client, "/api/v1/agents")
	case "dashboard export", "dashboard import", "slo apply", "test run", "test watch", "agent install":
		return exitErr(g, 4, errors.New("comando reconhecido, mas requer módulo opcional não habilitado neste perfil"))
	default:
		usage()
		return exitErr(g, 3, fmt.Errorf("comando desconhecido: %s", strings.Join(args, " ")))
	}
}
func parseGlobals(args []string) (globals, []string, error) {
	g := globals{output: "human", timeout: 30 * time.Second}
	fs := flag.NewFlagSet("sentinelctl", flag.ContinueOnError)
	fs.StringVar(&g.output, "output", "human", "human|json|yaml")
	fs.DurationVar(&g.timeout, "timeout", 30*time.Second, "timeout")
	fs.BoolVar(&g.noColor, "no-color", false, "sem cores")
	fs.BoolVar(&g.quiet, "quiet", false, "silencioso")
	if err := fs.Parse(args); err != nil {
		return g, nil, err
	}
	return g, fs.Args(), nil
}
func cmdLogin(ctx context.Context, g globals, c *apiclient.Client, path string, cfg *configFile, args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	user := fs.String("username", "admin", "usuário")
	password := fs.String("password", os.Getenv("SENTINEL_PASSWORD"), "senha")
	if err := fs.Parse(args); err != nil {
		return exitErr(g, 3, err)
	}
	if *password == "" {
		return exitErr(g, 5, errors.New("use --password ou SENTINEL_PASSWORD"))
	}
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := c.Do(ctx, "POST", "/api/v1/auth/login", map[string]string{"username": *user, "password": *password}, nil, &out); err != nil {
		return exitErr(g, 5, err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]cliContext{}
	}
	if cfg.Current == "" {
		cfg.Current = "local"
	}
	cfg.Contexts[cfg.Current] = cliContext{URL: c.BaseURL, Token: out.AccessToken}
	if err := saveConfig(path, *cfg); err != nil {
		return exitErr(g, 3, err)
	}
	return printOut(g, map[string]string{"status": "authenticated", "context": cfg.Current})
}
func cmdDoctor(ctx context.Context, g globals, c *apiclient.Client) int {
	var health, ready any
	e1 := c.Do(ctx, "GET", "/healthz", nil, nil, &health)
	e2 := c.Do(ctx, "GET", "/readyz", nil, nil, &ready)
	if e1 != nil || e2 != nil {
		return exitErr(g, 3, fmt.Errorf("health=%v readiness=%v", e1, e2))
	}
	return printOut(g, map[string]any{"api": health, "dependencies": ready, "version": version})
}
func get(ctx context.Context, g globals, c *apiclient.Client, path string) int {
	var out any
	if err := c.Do(ctx, "GET", path, nil, nil, &out); err != nil {
		return exitErr(g, 3, err)
	}
	return printOut(g, out)
}
func call(ctx context.Context, g globals, c *apiclient.Client, method, path string, body any, headers map[string]string) int {
	var out any
	if err := c.Do(ctx, method, path, body, headers, &out); err != nil {
		return exitErr(g, 3, err)
	}
	return printOut(g, out)
}
func applyFile(ctx context.Context, g globals, c *apiclient.Client, path string, args []string) int {
	file, err := fileArg(args)
	if err != nil {
		return exitErr(g, 3, err)
	}
	var body any
	if err = readYAML(file, &body); err != nil {
		return exitErr(g, 3, err)
	}
	return call(ctx, g, c, "POST", path, body, nil)
}
func validateFile(g globals, args []string) int {
	file, err := fileArg(args)
	if err != nil {
		return exitErr(g, 3, err)
	}
	var body map[string]any
	if err = readYAML(file, &body); err != nil {
		return exitErr(g, 3, err)
	}
	if len(body) == 0 {
		return exitErr(g, 3, errors.New("documento vazio"))
	}
	return printOut(g, map[string]any{"valid": true, "file": file})
}
func cmdRelease(ctx context.Context, g globals, c *apiclient.Client, args []string) int {
	fs := flag.NewFlagSet("release register", flag.ContinueOnError)
	service := fs.String("service", "", "")
	environment := fs.String("environment", "", "")
	ver := fs.String("version", "", "")
	sha := fs.String("commit-sha", "", "")
	digest := fs.String("image-digest", "", "")
	if err := fs.Parse(args); err != nil {
		return exitErr(g, 3, err)
	}
	if *service == "" || *environment == "" || *ver == "" {
		return exitErr(g, 3, errors.New("--service, --environment e --version são obrigatórios"))
	}
	body := map[string]any{"service": *service, "environment": *environment, "version": *ver, "commitSha": *sha, "imageDigest": *digest, "deployedAt": time.Now().UTC()}
	return call(ctx, g, c, "POST", "/api/v1/releases", body, map[string]string{"Idempotency-Key": fmt.Sprintf("%s-%s-%s", *service, *environment, *ver)})
}
func cmdReleaseValidate(ctx context.Context, g globals, c *apiclient.Client, args []string) int {
	fs := flag.NewFlagSet("release validate", flag.ContinueOnError)
	id := fs.String("release-id", "", "")
	mode := fs.String("mode", "standard", "")
	if err := fs.Parse(args); err != nil {
		return exitErr(g, 3, err)
	}
	if *id == "" {
		return exitErr(g, 3, errors.New("--release-id é obrigatório"))
	}
	return call(ctx, g, c, "POST", "/api/v1/releases/"+*id+"/validate", map[string]string{"mode": *mode}, nil)
}
func cmdWait(ctx context.Context, g globals, c *apiclient.Client, args []string) int {
	if len(args) == 0 {
		return exitErr(g, 3, errors.New("validation wait requer id"))
	}
	id := args[0]
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var out map[string]any
		if err := c.Do(ctx, "GET", "/api/v1/validations/"+id, nil, nil, &out); err != nil {
			return exitErr(g, 3, err)
		}
		if out["status"] == "COMPLETED" || out["status"] == "CANCELLED" {
			result, _ := out["result"].(string)
			_ = printOut(g, out)
			switch result {
			case "FAIL":
				return 2
			case "INCONCLUSIVE":
				return 4
			case "CANCELLED":
				return 3
			default:
				return 0
			}
		}
		select {
		case <-ctx.Done():
			return exitErr(g, 3, ctx.Err())
		case <-ticker.C:
		}
	}
}
func fileArg(args []string) (string, error) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-f" || args[i] == "--file" {
			return args[i+1], nil
		}
	}
	return "", errors.New("use -f <arquivo>")
}
func readYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}
func loadConfig(path string) (configFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return configFile{Current: "local", Contexts: map[string]cliContext{"local": {URL: "http://localhost:8080"}}}, nil
	}
	if err != nil {
		return configFile{}, err
	}
	var c configFile
	err = yaml.Unmarshal(data, &c)
	return c, err
}
func saveConfig(path string, c configFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
func configPath() string {
	if p := os.Getenv("SENTINEL_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".sentinelops.yaml"
	}
	return filepath.Join(dir, "sentinelops", "config.yaml")
}
func printOut(g globals, v any) int {
	if g.quiet {
		return 0
	}
	var data []byte
	var err error
	switch g.output {
	case "json":
		data, err = json.MarshalIndent(v, "", "  ")
	case "yaml":
		data, err = yaml.Marshal(v)
	case "human":
		data, err = yaml.Marshal(v)
	default:
		return exitErr(g, 3, errors.New("--output deve ser human, json ou yaml"))
	}
	if err != nil {
		return exitErr(g, 3, err)
	}
	fmt.Print(string(data))
	return 0
}
func exitErr(g globals, code int, err error) int {
	if !g.quiet {
		fmt.Fprintln(os.Stderr, "sentinelctl:", err)
	}
	return code
}
func usage() {
	fmt.Fprintln(os.Stderr, "sentinelctl [--output human|json|yaml] [--timeout 30s] <resource> <command>")
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
