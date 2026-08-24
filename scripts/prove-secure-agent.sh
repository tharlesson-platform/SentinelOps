#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
API_URL=${SENTINEL_API_URL:-http://127.0.0.1:8080}
GATEWAY_HOST=${SENTINEL_INGEST_SERVER_NAME:-ingest.local}
GATEWAY_PORT=${SENTINEL_INGEST_HTTPS_PORT:-8443}
CERT_DIR=${SENTINEL_PROOF_CERT_DIR:-$ROOT/.sentinelops/proof-agent}
AGENT_NAME=${SENTINEL_PROOF_AGENT_NAME:-proof-agent}
CA_DIR=${SENTINEL_PROOF_CA_DIR:-$ROOT/deploy/gateway/pki}
CA_PASSPHRASE_FILE=${SENTINEL_PROOF_CA_PASSPHRASE_FILE:-$ROOT/.sentinelops/secrets/pki-ca-passphrase}

# shellcheck disable=SC1091
. "$ROOT/.env"
for dependency in curl jq openssl docker; do
  command -v "$dependency" >/dev/null 2>&1 || { echo "$dependency é obrigatório" >&2; exit 1; }
done
for file in ca.crt client.crt client.key; do
  [ -s "$CERT_DIR/$file" ] || { echo "Certificado ausente: $CERT_DIR/$file" >&2; exit 1; }
done
[ -s "$CA_PASSPHRASE_FILE" ] || { echo "Senha local da CA ausente: $CA_PASSPHRASE_FILE" >&2; exit 1; }

proof_dir=$(mktemp -d "$ROOT/artifacts/secure-agent.XXXXXX")
cleanup() {
  # Estes arquivos temporários contêm credenciais exibidas apenas uma vez.
  find "$proof_dir" -type f -exec sh -c 'dd if=/dev/zero of="$1" bs=4096 count=1 conv=notrunc >/dev/null 2>&1 || true' _ {} \;
  find "$proof_dir" -depth -delete
}
trap cleanup EXIT HUP INT TERM

login_payload=$(jq -nc --arg username "$LOCAL_ADMIN_USER" --arg password "$LOCAL_ADMIN_PASSWORD" '{username:$username,password:$password}')
access_token=$(curl -fsS -X POST "$API_URL/api/v1/auth/login" -H 'Content-Type: application/json' --data "$login_payload" | jq -er .data.accessToken)
bootstrap_payload=$(jq -nc --arg agentName "$AGENT_NAME" '{agentName:$agentName,ttl:"10m"}')
bootstrap_response=$(curl -fsS -X POST "$API_URL/api/v1/agent-bootstrap-tokens" \
  -H "Authorization: Bearer $access_token" -H 'Content-Type: application/json' \
  --data "$bootstrap_payload")
bootstrap_token=$(printf '%s' "$bootstrap_response" | jq -er .data.token)
organization_id=$(printf '%s' "$bootstrap_response" | jq -er .data.organizationId)
openssl x509 -in "$CERT_DIR/client.crt" -noout -ext subjectAltName | \
  grep -Fq "URI:spiffe://sentinelops/organizations/$organization_id/collectors/$AGENT_NAME" || {
    echo "certificado de prova não está vinculado à organização do token" >&2
    exit 1
  }
register_payload=$(jq -nc --arg name "$AGENT_NAME" '{name:$name,environment:"production",team:"sre",location:"local",capabilities:["http","k6"]}')

curl_mtls() {
  curl -sS --resolve "$GATEWAY_HOST:$GATEWAY_PORT:127.0.0.1" \
    --cacert "$CERT_DIR/ca.crt" --cert "$CERT_DIR/client.crt" --key "$CERT_DIR/client.key" "$@"
}

register_code=$(curl_mtls -o "$proof_dir/register.json" -w '%{http_code}' -X POST \
  "https://$GATEWAY_HOST:$GATEWAY_PORT/api/v1/agents/register" \
  -H "Authorization: Bearer $bootstrap_token" -H 'Content-Type: application/json' --data "$register_payload")
if [ "$register_code" != 201 ]; then
  printf 'register_http=%s error=%s\n' "$register_code" "$(jq -r '.error.code // "invalid-response"' "$proof_dir/register.json" 2>/dev/null || printf invalid-response)" >&2
  exit 1
fi
agent_id=$(jq -er .data.id "$proof_dir/register.json")
agent_token=$(jq -er .data.token "$proof_dir/register.json")
reuse_code=$(curl_mtls -o /dev/null -w '%{http_code}' -X POST \
  "https://$GATEWAY_HOST:$GATEWAY_PORT/api/v1/agents/register" \
  -H "Authorization: Bearer $bootstrap_token" -H 'Content-Type: application/json' --data "$register_payload")
heartbeat_code=$(curl_mtls -o /dev/null -w '%{http_code}' -X POST \
  "https://$GATEWAY_HOST:$GATEWAY_PORT/api/v1/agents/$agent_id/heartbeat" \
  -H "X-Agent-Token: $agent_token" -H 'Content-Type: application/json' --data '{"proof":"mtls"}')

# Um segundo token permite provar que headers forjados, nome divergente e uma
# identidade de outra organização são rejeitados antes de consumir o token.
cross_agent_name="proof-cross-tenant"
cross_payload=$(jq -nc --arg agentName "$cross_agent_name" '{agentName:$agentName,ttl:"1m"}')
cross_response=$(curl -fsS -X POST "$API_URL/api/v1/agent-bootstrap-tokens" \
  -H "Authorization: Bearer $access_token" -H 'Content-Type: application/json' --data "$cross_payload")
cross_token=$(printf '%s' "$cross_response" | jq -er .data.token)
cross_register_payload=$(jq -nc --arg name "$cross_agent_name" '{name:$name,environment:"production",team:"sre",capabilities:["http"]}')
fake_fingerprint=$(printf '%064d' 0 | tr '0' 'a')

spoof_code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$API_URL/api/v1/agents/register" \
  -H "Authorization: Bearer $cross_token" -H 'Content-Type: application/json' \
  -H "X-Sentinel-Client-Cert-Fingerprint: $fake_fingerprint" \
  -H "X-Sentinel-Client-Organization: $organization_id" -H "X-Sentinel-Client-Name: $cross_agent_name" \
  -H 'X-Sentinel-MTLS-Proxy-Authorization: forged' --data "$cross_register_payload")

name_mismatch_code=$(curl_mtls -o /dev/null -w '%{http_code}' -X POST \
  "https://$GATEWAY_HOST:$GATEWAY_PORT/api/v1/agents/register" \
  -H "Authorization: Bearer $cross_token" -H 'Content-Type: application/json' --data "$cross_register_payload")

other_uuid_raw=$(openssl rand -hex 16)
other_organization=$(printf '%s' "$other_uuid_raw" | sed -E 's/^(.{8})(.{4})(.{4})(.{4})(.{12})$/\1-\2-\3-\4-\5/')
mkdir -p "$proof_dir/other" "$proof_dir/legitimate"
"$ROOT/scripts/bootstrap-pki.sh" issue-collector --ca-dir "$CA_DIR" --passphrase-file "$CA_PASSPHRASE_FILE" \
  --organization "$other_organization" --name "$cross_agent_name" --output-dir "$proof_dir/other" >/dev/null
cross_tenant_code=$(curl -sS --resolve "$GATEWAY_HOST:$GATEWAY_PORT:127.0.0.1" \
  --cacert "$proof_dir/other/ca.crt" --cert "$proof_dir/other/client.crt" --key "$proof_dir/other/client.key" \
  -o /dev/null -w '%{http_code}' -X POST "https://$GATEWAY_HOST:$GATEWAY_PORT/api/v1/agents/register" \
  -H "Authorization: Bearer $cross_token" -H 'Content-Type: application/json' --data "$cross_register_payload")

"$ROOT/scripts/bootstrap-pki.sh" issue-collector --ca-dir "$CA_DIR" --passphrase-file "$CA_PASSPHRASE_FILE" \
  --organization "$organization_id" --name "$cross_agent_name" --output-dir "$proof_dir/legitimate" >/dev/null
legitimate_code=$(curl -sS --resolve "$GATEWAY_HOST:$GATEWAY_PORT:127.0.0.1" \
  --cacert "$proof_dir/legitimate/ca.crt" --cert "$proof_dir/legitimate/client.crt" --key "$proof_dir/legitimate/client.key" \
  -o "$proof_dir/cross-register.json" -w '%{http_code}' -X POST "https://$GATEWAY_HOST:$GATEWAY_PORT/api/v1/agents/register" \
  -H "Authorization: Bearer $cross_token" -H 'Content-Type: application/json' --data "$cross_register_payload")

set +e
curl -sS --resolve "$GATEWAY_HOST:$GATEWAY_PORT:127.0.0.1" --cacert "$CERT_DIR/ca.crt" \
  -o /dev/null "https://$GATEWAY_HOST:$GATEWAY_PORT/v1/traces" 2>"$proof_dir/no-cert.err"
no_cert_exit=$?
set -e

fingerprint=$(openssl x509 -in "$CERT_DIR/client.crt" -noout -fingerprint -sha256 | cut -d= -f2 | tr -d ':' | tr '[:upper:]' '[:lower:]')
db_match=$(printf "SELECT count(*)=1 FROM agents WHERE id=:'agent_id'::uuid AND client_cert_fingerprint=:'fingerprint' AND revoked_at IS NULL;\n" | \
  docker compose --env-file "$ROOT/.env" -f "$ROOT/deploy/compose/docker-compose.yml" exec -T postgres \
    psql -U sentinel -d sentinel -Atq -v agent_id="$agent_id" -v fingerprint="$fingerprint")

printf 'register_http=%s\nbootstrap_reuse_http=%s\nheartbeat_http=%s\nspoofed_proxy_headers_http=%s\ncertificate_name_mismatch_http=%s\ncross_tenant_certificate_http=%s\nlegitimate_tenant_certificate_http=%s\nno_client_cert_exit=%s\ndatabase_fingerprint_bound=%s\n' \
  "$register_code" "$reuse_code" "$heartbeat_code" "$spoof_code" "$name_mismatch_code" "$cross_tenant_code" "$legitimate_code" "$no_cert_exit" "$db_match"
[ "$register_code" = 201 ] && [ "$reuse_code" = 401 ] && [ "$heartbeat_code" = 200 ] && \
  [ "$spoof_code" = 401 ] && [ "$name_mismatch_code" = 401 ] && [ "$cross_tenant_code" = 401 ] && \
  [ "$legitimate_code" = 201 ] && [ "$no_cert_exit" -ne 0 ] && [ "$db_match" = t ]
