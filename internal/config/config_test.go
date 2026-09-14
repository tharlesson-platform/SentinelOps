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
	t.Setenv("OIDC_API_AUDIENCE", "sentinelops-api")
	t.Setenv("AGENT_BOOTSTRAP_TOKEN", strings.Repeat("a", 64))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "forbidden in production") {
		t.Fatalf("expected production bootstrap rejection, got %v", err)
	}
}

func TestWebhookSecretsRequireStrongPerTenantValues(t *testing.T) {
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops")
	t.Setenv("OIDC_API_AUDIENCE", "sentinelops-api")
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
	t.Setenv("OIDC_API_AUDIENCE", "sentinelops-api")
	t.Setenv("AGENT_BOOTSTRAP_TOKEN", "")
	t.Setenv("MTLS_PROXY_SHARED_SECRET", "short")
	t.Setenv("TELEMETRY_QUERY_GATEWAY_URL", "https://query-gateway.internal:8444")
	t.Setenv("TELEMETRY_QUERY_API_CLIENT_CERT_FILE", "/certs/api.crt")
	t.Setenv("TELEMETRY_QUERY_API_CLIENT_KEY_FILE", "/certs/api.key")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected common configuration error: %v", err)
	}
	if err = cfg.ValidateAPI(); err == nil || !strings.Contains(err.Error(), "MTLS_PROXY_SHARED_SECRET") {
		t.Fatalf("expected weak mTLS proxy secret rejection, got %v", err)
	}
}

func TestProductionAPIRequiresMTLSQueryGateway(t *testing.T) {
	t.Setenv("SENTINEL_ENV", "production")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops")
	t.Setenv("OIDC_API_AUDIENCE", "sentinelops-api")
	t.Setenv("MTLS_PROXY_SHARED_SECRET", strings.Repeat("m", 32))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.ValidateAPI(); err == nil || !strings.Contains(err.Error(), "TELEMETRY_QUERY_GATEWAY_URL") {
		t.Fatalf("expected production API query gateway rejection, got %v", err)
	}
	t.Setenv("TELEMETRY_QUERY_GATEWAY_URL", "https://query-gateway.internal:8444")
	t.Setenv("TELEMETRY_QUERY_API_CLIENT_CERT_FILE", "/certs/api.crt")
	t.Setenv("TELEMETRY_QUERY_API_CLIENT_KEY_FILE", "/certs/api.key")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.ValidateAPI(); err != nil {
		t.Fatalf("expected production API mTLS configuration to pass: %v", err)
	}
}

func TestProductionWorkerRequiresMTLSQueryGateway(t *testing.T) {
	t.Setenv("SENTINEL_ENV", "production")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops")
	t.Setenv("OIDC_API_AUDIENCE", "sentinelops-api")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected common configuration error: %v", err)
	}
	if err = cfg.ValidateWorker(); err == nil || !strings.Contains(err.Error(), "TELEMETRY_QUERY_GATEWAY_URL") {
		t.Fatalf("expected query gateway rejection, got %v", err)
	}
	t.Setenv("TELEMETRY_QUERY_GATEWAY_URL", "https://query-gateway.internal:8444")
	t.Setenv("TELEMETRY_QUERY_CLIENT_CERT_FILE", "/certs/tls.crt")
	t.Setenv("TELEMETRY_QUERY_CLIENT_KEY_FILE", "/certs/tls.key")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.ValidateWorker(); err != nil {
		t.Fatalf("expected worker mTLS configuration to pass: %v", err)
	}
}

func TestNotificationWebhooksNeedAllowedHostsAndHTTPSInProduction(t *testing.T) {
	t.Setenv("SENTINEL_ENV", "production")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops")
	t.Setenv("OIDC_API_AUDIENCE", "sentinelops-api")
	t.Setenv("NOTIFICATION_WEBHOOK_URLS", `{"teams":"http://hooks.example.com/x"}`)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NOTIFICATION_ALLOWED_HOSTS") {
		t.Fatalf("expected allowlist rejection, got %v", err)
	}
	t.Setenv("NOTIFICATION_ALLOWED_HOSTS", "hooks.example.com")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("expected HTTPS rejection, got %v", err)
	}
}

func TestEmptyNotificationWebhookObjectDoesNotNeedAllowedHosts(t *testing.T) {
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops")
	t.Setenv("OIDC_API_AUDIENCE", "sentinelops-api")
	t.Setenv("NOTIFICATION_WEBHOOK_URLS", `{}`)
	t.Setenv("NOTIFICATION_ALLOWED_HOSTS", "")
	if _, err := Load(); err != nil {
		t.Fatalf("empty notification webhook object must not require an allowlist: %v", err)
	}
}

func TestOIDCRequiresAPIAudience(t *testing.T) {
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops-web")
	t.Setenv("OIDC_API_AUDIENCE", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "OIDC_API_AUDIENCE") {
		t.Fatalf("expected OIDC API audience rejection, got %v", err)
	}
}

func TestTenantRequestQuotaMustBeBounded(t *testing.T) {
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "sentinelops")
	t.Setenv("OIDC_API_AUDIENCE", "sentinelops-api")
	t.Setenv("TENANT_REQUESTS_PER_MINUTE", "0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TENANT_REQUESTS_PER_MINUTE") {
		t.Fatalf("expected tenant quota rejection, got %v", err)
	}
	t.Setenv("TENANT_REQUESTS_PER_MINUTE", "42")
	t.Setenv("CATALOG_QUERIES_PER_MINUTE", "0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CATALOG_QUERIES_PER_MINUTE") {
		t.Fatalf("expected catalog quota rejection, got %v", err)
	}
	t.Setenv("CATALOG_QUERIES_PER_MINUTE", "24")
	if cfg, err := Load(); err != nil || cfg.TenantRequestsPerMinute != 42 || cfg.CatalogQueriesPerMinute != 24 {
		t.Fatalf("expected tenant quota configuration, got %#v, %v", cfg, err)
	}
}
