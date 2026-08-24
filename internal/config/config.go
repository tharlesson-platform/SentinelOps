package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr          string
	DatabaseURL       string
	TemporalAddress   string
	TemporalNamespace string
	AuthMode          string
	JWTSecret         string
	LocalUser         string
	LocalPasswordHash string
	OIDCIssuerURL     string
	OIDCClientID      string
	Environment       string
	AllowedOrigin     string
	ArtifactDir       string
	AgentBootstrap    string
	MTLSProxySecret   string
	WebhookSecret     string
	WebhookSecrets    map[string]string
	RequestTimeout    time.Duration
	MaxBodyBytes      int64
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          env("SENTINEL_HTTP_ADDR", ":8080"),
		DatabaseURL:       env("DATABASE_URL", "postgres://sentinel_app:sentinel_app@localhost:5432/sentinel?sslmode=disable"),
		TemporalAddress:   env("TEMPORAL_ADDRESS", "localhost:7233"),
		TemporalNamespace: env("TEMPORAL_NAMESPACE", "default"),
		AuthMode:          env("AUTH_MODE", "local"),
		JWTSecret:         os.Getenv("JWT_SECRET"),
		LocalUser:         env("LOCAL_ADMIN_USER", "admin"),
		LocalPasswordHash: os.Getenv("LOCAL_ADMIN_PASSWORD_HASH"),
		OIDCIssuerURL:     os.Getenv("OIDC_ISSUER_URL"),
		OIDCClientID:      os.Getenv("OIDC_CLIENT_ID"),
		Environment:       env("SENTINEL_ENV", "development"),
		AllowedOrigin:     env("ALLOWED_ORIGIN", "http://localhost:3000"),
		ArtifactDir:       env("ARTIFACT_DIR", "/var/lib/sentinelops/artifacts"),
		AgentBootstrap:    os.Getenv("AGENT_BOOTSTRAP_TOKEN"),
		MTLSProxySecret:   os.Getenv("MTLS_PROXY_SHARED_SECRET"),
		WebhookSecret:     os.Getenv("WEBHOOK_HMAC_SECRET"),
		RequestTimeout:    duration("REQUEST_TIMEOUT", 15*time.Second),
		MaxBodyBytes:      int64Value("MAX_BODY_BYTES", 1<<20),
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
	if cfg.AuthMode == "oidc" && (cfg.OIDCIssuerURL == "" || cfg.OIDCClientID == "") {
		return Config{}, fmt.Errorf("OIDC auth requires OIDC_ISSUER_URL and OIDC_CLIENT_ID")
	}
	if cfg.AuthMode != "local" && cfg.AuthMode != "oidc" {
		return Config{}, fmt.Errorf("AUTH_MODE must be local or oidc")
	}
	return cfg, nil
}

// ValidateAPI checks requirements that apply only to the HTTP API process.
// Workers share Config but must not receive the API-gateway trust secret.
func (cfg Config) ValidateAPI() error {
	if cfg.Environment == "production" && len(cfg.MTLSProxySecret) < 32 {
		return fmt.Errorf("MTLS_PROXY_SHARED_SECRET must have at least 32 bytes in production")
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
