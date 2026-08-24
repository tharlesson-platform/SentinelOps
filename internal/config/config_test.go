package config

import (
	"strings"
	"testing"
)

func TestProductionRejectsDevelopmentBootstrapToken(t *testing.T) {
	t.Setenv("SENTINEL_ENV", "production")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops")
	t.Setenv("AGENT_BOOTSTRAP_TOKEN", strings.Repeat("a", 64))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "forbidden in production") {
		t.Fatalf("expected production bootstrap rejection, got %v", err)
	}
}

func TestWebhookSecretsRequireStrongPerTenantValues(t *testing.T) {
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops")
	t.Setenv("WEBHOOK_HMAC_SECRETS", `{"tenant-a":"short"}`)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("expected weak webhook secret rejection, got %v", err)
	}
}

func TestProductionRequiresStrongMTLSProxySecret(t *testing.T) {
	t.Setenv("SENTINEL_ENV", "production")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops")
	t.Setenv("AGENT_BOOTSTRAP_TOKEN", "")
	t.Setenv("MTLS_PROXY_SHARED_SECRET", "short")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected common configuration error: %v", err)
	}
	if err = cfg.ValidateAPI(); err == nil || !strings.Contains(err.Error(), "MTLS_PROXY_SHARED_SECRET") {
		t.Fatalf("expected weak mTLS proxy secret rejection, got %v", err)
	}
}
