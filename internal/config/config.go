package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                 string
	DatabaseURL              string
	TemporalAddress          string
	TemporalNamespace        string
	AuthMode                 string
	JWTSecret                string
	LocalUser                string
	LocalPasswordHash        string
	OIDCIssuerURL            string
	OIDCClientID             string
	OIDCAudience             string
	OIDCRequiredScope        string
	Environment              string
	AllowedOrigin            string
	ArtifactDir              string
	AgentBootstrap           string
	MTLSProxySecret          string
	WebhookSecret            string
	WebhookSecrets           map[string]string
	NotificationWebhookURLs  map[string]string
	NotificationAllowedHosts []string
	TelemetryQueryGatewayURL string
	TelemetryQueryClientCert string
	TelemetryQueryClientKey  string
	TelemetryQueryAPICert    string
	TelemetryQueryAPIKey     string
	PrometheusURL            string
	LokiURL                  string
	TempoURL                 string
	PyroscopeURL             string
	RequestTimeout           time.Duration
	MaxBodyBytes             int64
	TenantRequestsPerMinute  int
	CatalogQueriesPerMinute  int
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:                 env("SENTINEL_HTTP_ADDR", ":8080"),
		DatabaseURL:              env("DATABASE_URL", "postgres://sentinel_app:sentinel_app@localhost:5432/sentinel?sslmode=disable"),
		TemporalAddress:          env("TEMPORAL_ADDRESS", "localhost:7233"),
		TemporalNamespace:        env("TEMPORAL_NAMESPACE", "default"),
		AuthMode:                 env("AUTH_MODE", "local"),
		JWTSecret:                os.Getenv("JWT_SECRET"),
		LocalUser:                env("LOCAL_ADMIN_USER", "admin"),
		LocalPasswordHash:        os.Getenv("LOCAL_ADMIN_PASSWORD_HASH"),
		OIDCIssuerURL:            os.Getenv("OIDC_ISSUER_URL"),
		OIDCClientID:             os.Getenv("OIDC_CLIENT_ID"),
		OIDCAudience:             os.Getenv("OIDC_API_AUDIENCE"),
		OIDCRequiredScope:        env("OIDC_REQUIRED_SCOPE", "sentinelops.api"),
		Environment:              env("SENTINEL_ENV", "development"),
		AllowedOrigin:            env("ALLOWED_ORIGIN", "http://localhost:3000"),
		ArtifactDir:              env("ARTIFACT_DIR", "/var/lib/sentinelops/artifacts"),
		AgentBootstrap:           os.Getenv("AGENT_BOOTSTRAP_TOKEN"),
		MTLSProxySecret:          os.Getenv("MTLS_PROXY_SHARED_SECRET"),
		WebhookSecret:            os.Getenv("WEBHOOK_HMAC_SECRET"),
		NotificationAllowedHosts: splitList(os.Getenv("NOTIFICATION_ALLOWED_HOSTS")),
		TelemetryQueryGatewayURL: env("TELEMETRY_QUERY_GATEWAY_URL", ""),
		TelemetryQueryClientCert: env("TELEMETRY_QUERY_CLIENT_CERT_FILE", ""),
		TelemetryQueryClientKey:  env("TELEMETRY_QUERY_CLIENT_KEY_FILE", ""),
		TelemetryQueryAPICert:    env("TELEMETRY_QUERY_API_CLIENT_CERT_FILE", ""),
		TelemetryQueryAPIKey:     env("TELEMETRY_QUERY_API_CLIENT_KEY_FILE", ""),
		PrometheusURL:            env("PROMETHEUS_URL", "http://localhost:9090"),
		LokiURL:                  env("LOKI_URL", "http://localhost:3100"),
		TempoURL:                 env("TEMPO_URL", "http://localhost:3200"),
		PyroscopeURL:             env("PYROSCOPE_URL", "http://localhost:4040"),
		RequestTimeout:           duration("REQUEST_TIMEOUT", 15*time.Second),
		MaxBodyBytes:             int64Value("MAX_BODY_BYTES", 1<<20),
		TenantRequestsPerMinute:  intValue("TENANT_REQUESTS_PER_MINUTE", 600),
		CatalogQueriesPerMinute:  intValue("CATALOG_QUERIES_PER_MINUTE", 120),
	}
	if cfg.AuthMode == "local" {
		if len(cfg.JWTSecret) < 32 || cfg.LocalPasswordHash == "" {
			return Config{}, fmt.Errorf("local auth requires JWT_SECRET (>=32 bytes) and LOCAL_ADMIN_PASSWORD_HASH")
		}
		if cfg.Environment == "production" {
			return Config{}, fmt.Errorf("AUTH_MODE=local is forbidden in production")
		}
	}
	if cfg.Environment == "production" && cfg.AgentBootstrap != "" {
		return Config{}, fmt.Errorf("AGENT_BOOTSTRAP_TOKEN is a development seed and is forbidden in production; issue one-time tokens through the authenticated API")
	}
	if raw := os.Getenv("WEBHOOK_HMAC_SECRETS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.WebhookSecrets); err != nil {
			return Config{}, fmt.Errorf("WEBHOOK_HMAC_SECRETS must be a JSON object: %w", err)
		}
	}
	if raw := os.Getenv("NOTIFICATION_WEBHOOK_URLS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.NotificationWebhookURLs); err != nil {
			return Config{}, fmt.Errorf("NOTIFICATION_WEBHOOK_URLS must be a JSON object: %w", err)
		}
		if len(cfg.NotificationWebhookURLs) > 0 && len(cfg.NotificationAllowedHosts) == 0 {
			return Config{}, fmt.Errorf("NOTIFICATION_ALLOWED_HOSTS is required when notification webhooks are configured")
		}
		for name, endpoint := range cfg.NotificationWebhookURLs {
			if name == "" || endpoint == "" {
				return Config{}, fmt.Errorf("notification webhook reference and URL must not be empty")
			}
			if cfg.Environment == "production" && !strings.HasPrefix(endpoint, "https://") {
				return Config{}, fmt.Errorf("notification webhook %q must use HTTPS in production", name)
			}
		}
	}
	if cfg.WebhookSecret != "" {
		if cfg.Environment == "production" {
			return Config{}, fmt.Errorf("WEBHOOK_HMAC_SECRET is single-tenant and forbidden in production; use WEBHOOK_HMAC_SECRETS")
		}
		if cfg.WebhookSecrets == nil {
			cfg.WebhookSecrets = map[string]string{}
		}
		cfg.WebhookSecrets["local"] = cfg.WebhookSecret
	}
	for organization, secret := range cfg.WebhookSecrets {
		if organization == "" || len(secret) < 32 {
			return Config{}, fmt.Errorf("webhook secret for organization %q must have at least 32 bytes", organization)
		}
	}
	if cfg.AuthMode == "oidc" && (cfg.OIDCIssuerURL == "" || cfg.OIDCClientID == "" || cfg.OIDCAudience == "") {
		return Config{}, fmt.Errorf("OIDC auth requires OIDC_ISSUER_URL, OIDC_CLIENT_ID and OIDC_API_AUDIENCE")
	}
	if cfg.AuthMode != "local" && cfg.AuthMode != "oidc" {
		return Config{}, fmt.Errorf("AUTH_MODE must be local or oidc")
	}
	if cfg.TenantRequestsPerMinute < 1 || cfg.TenantRequestsPerMinute > 100000 {
		return Config{}, fmt.Errorf("TENANT_REQUESTS_PER_MINUTE must be between 1 and 100000")
	}
	if cfg.CatalogQueriesPerMinute < 1 || cfg.CatalogQueriesPerMinute > 100000 {
		return Config{}, fmt.Errorf("CATALOG_QUERIES_PER_MINUTE must be between 1 and 100000")
	}
	return cfg, nil
}

// ValidateAPI checks requirements that apply only to the HTTP API process.
// Workers share Config but must not receive the API-gateway trust secret.
func (cfg Config) ValidateAPI() error {
	if cfg.Environment == "production" && len(cfg.MTLSProxySecret) < 32 {
		return fmt.Errorf("MTLS_PROXY_SHARED_SECRET must have at least 32 bytes in production")
	}
	if cfg.Environment == "production" {
		if err := validateQueryGateway(cfg.TelemetryQueryGatewayURL, cfg.TelemetryQueryAPICert, cfg.TelemetryQueryAPIKey, "API"); err != nil {
			return err
		}
	}
	if cfg.AuthMode == "oidc" && cfg.OIDCRequiredScope == "" {
		return fmt.Errorf("OIDC_REQUIRED_SCOPE is required for OIDC API authorization")
	}
	return nil
}

// ValidateWorker rejects direct telemetry queries in production. The gateway
// authenticates the release-validator workload by mTLS and is the only
// component allowed to inject the organization header into telemetry backends.
func (cfg Config) ValidateWorker() error {
	if cfg.Environment != "production" {
		return nil
	}
	return validateQueryGateway(cfg.TelemetryQueryGatewayURL, cfg.TelemetryQueryClientCert, cfg.TelemetryQueryClientKey, "worker")
}

func validateQueryGateway(rawURL, certFile, keyFile, workload string) error {
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil {
		return fmt.Errorf("TELEMETRY_QUERY_GATEWAY_URL HTTPS is required in production")
	}
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("telemetry query mTLS certificate and key are required for production %s", workload)
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func int64Value(key string, fallback int64) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func intValue(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
		return 0
	}
	return fallback
}

func splitList(value string) []string {
	items := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
