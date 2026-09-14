#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
ENV_FILE="$ROOT/.env"

ensure_default() {
  key=$1
  value=$2
  grep -q "^${key}=" "$ENV_FILE" || printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
}

if [ -f "$ENV_FILE" ]; then
  # Existing installations can predate new non-secret runtime settings. Keep
  # every secret byte intact and append only missing documented defaults.
  umask 077
  ensure_default OIDC_API_AUDIENCE sentinelops-api
  ensure_default OIDC_REQUIRED_SCOPE sentinelops.api
  ensure_default TENANT_REQUESTS_PER_MINUTE 600
  ensure_default CATALOG_QUERIES_PER_MINUTE 120
  ensure_default NOTIFICATION_WEBHOOK_URLS '{}'
  ensure_default NOTIFICATION_ALLOWED_HOSTS ''
  ensure_default SENTINEL_GRAFANA_BIND_ADDRESS 127.0.0.1
  ensure_default SENTINEL_KEYCLOAK_BIND_ADDRESS 127.0.0.1
  ensure_default SENTINEL_TEMPORAL_UI_BIND_ADDRESS 127.0.0.1
  ensure_default SENTINEL_TEMPO_BIND_ADDRESS 127.0.0.1
  ensure_default SENTINEL_PYROSCOPE_BIND_ADDRESS 127.0.0.1
  ensure_default SYNTHETIC_ALLOWED_TARGETS 'app.example.com@203.0.113.10/32'
  ensure_default RELEASE_VALIDATION_ALLOWED_HOSTS app.example.com
  ensure_default RELEASE_VALIDATION_BASE_URL https://app.example.com
  chmod 600 "$ENV_FILE"
  echo "SentinelOps já possui .env; defaults ausentes foram reconciliados sem sobrescrever secrets."
  exit 0
fi

random_hex() { openssl rand -hex "$1"; }
ADMIN_PASSWORD=$(openssl rand -base64 24 | tr -d '/+=' | cut -c1-24)
ADMIN_HASH=$(htpasswd -bnBC 12 sentinel "$ADMIN_PASSWORD" | cut -d: -f2)

umask 077
apply_env() {
  sed \
    -e "s|__POSTGRES_PASSWORD__|$(random_hex 24)|" \
    -e "s|__JWT_SECRET__|$(random_hex 32)|" \
    -e "s|__LOCAL_ADMIN_PASSWORD__|$ADMIN_PASSWORD|" \
    -e "s|__LOCAL_ADMIN_PASSWORD_HASH__|$ADMIN_HASH|" \
    -e "s|__AGENT_BOOTSTRAP_TOKEN__|$(random_hex 32)|" \
    -e "s|__MTLS_PROXY_SHARED_SECRET__|$(random_hex 32)|" \
    -e "s|__WEBHOOK_HMAC_SECRET__|$(random_hex 32)|" \
    -e "s|__MINIO_ROOT_USER__|sentinel$(random_hex 4)|" \
    -e "s|__MINIO_ROOT_PASSWORD__|$(random_hex 24)|" \
    -e "s|__GRAFANA_ADMIN_PASSWORD__|$(random_hex 18)|" \
    -e "s|__KEYCLOAK_ADMIN_PASSWORD__|$(random_hex 18)|" \
    "$ROOT/.env.example"
}
apply_env > "$ENV_FILE"
chmod 600 "$ENV_FILE"
echo "Configuração local criada em $ENV_FILE (modo 0600)."
echo "Usuário SentinelOps: admin"
echo "A senha não é impressa; use 'make credentials' conscientemente apenas neste host."
