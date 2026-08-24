#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
ENV_FILE="$ROOT/.env"

if [ -f "$ENV_FILE" ]; then
  echo "SentinelOps já possui .env; nenhum secret foi sobrescrito."
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
