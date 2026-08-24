#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
ORGANIZATION=""
COLLECTOR_NAME=""
GATEWAY_URL=""
TLS_SERVER_NAME=""
CA_DIR="$ROOT/deploy/gateway/pki"
PASSPHRASE_FILE="$ROOT/.sentinelops/secrets/pki-ca-passphrase"
OUTPUT_DIR="$ROOT/artifacts/collector-bundles"
ENVIRONMENT=production
TEAM=platform
LOCATION=on-premises
WITH_CONTAINERS=false

usage() {
  cat <<'EOF'
Uso: create-linux-collector-bundle.sh --organization ORG --collector-name NAME \
  --gateway-url https://ingest.example:8443 --tls-server-name ingest.example [opções]

  --ca-dir DIR
  --passphrase-file FILE       senha da CA em arquivo 0600
  --output-dir DIR
  --environment NAME
  --team NAME
  --location NAME
  --with-containers            coleta privilegiada de containers

Gera pacote autocontido com certificado mTLS exclusivo e Alloy corrigido.
A chave da CA e a senha nunca entram no pacote.
EOF
}

die() { printf '%s\n' "[sentinelops][erro] $*" >&2; exit 1; }
validate_name() { printf '%s' "$1" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._-]{1,62}$' || die "nome inválido: $1"; }

while [ "$#" -gt 0 ]; do
  case "$1" in
    --organization) ORGANIZATION=$2; shift 2 ;;
    --collector-name) COLLECTOR_NAME=$2; shift 2 ;;
    --gateway-url) GATEWAY_URL=$2; shift 2 ;;
    --tls-server-name) TLS_SERVER_NAME=$2; shift 2 ;;
    --ca-dir) CA_DIR=$2; shift 2 ;;
    --passphrase-file) PASSPHRASE_FILE=$2; shift 2 ;;
    --output-dir) OUTPUT_DIR=$2; shift 2 ;;
    --environment) ENVIRONMENT=$2; shift 2 ;;
    --team) TEAM=$2; shift 2 ;;
    --location) LOCATION=$2; shift 2 ;;
    --with-containers) WITH_CONTAINERS=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "Opção desconhecida: $1" ;;
  esac
done

for value in "$ORGANIZATION" "$COLLECTOR_NAME" "$TLS_SERVER_NAME" "$ENVIRONMENT" "$TEAM" "$LOCATION"; do validate_name "$value"; done
printf '%s' "$GATEWAY_URL" | grep -Eq '^https://[A-Za-z0-9._-]+(:[0-9]{1,5})?$' || die "--gateway-url deve ser uma origem HTTPS sem path"
if [ ! -s "$CA_DIR/ca.key" ] || [ ! -s "$CA_DIR/ca.crt" ]; then
  die "--ca-dir inválido"
fi
[ -s "$PASSPHRASE_FILE" ] || die "--passphrase-file inválido"
mkdir -p "$OUTPUT_DIR"
umask 077
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
bundle="$stage/sentinelops-collector-$COLLECTOR_NAME"
mkdir -p "$bundle/scripts/lib" "$bundle/deploy/agents/linux" "$bundle/deploy/alloy/patches"
cp "$ROOT/scripts/install-linux-collector.sh" "$bundle/scripts/"
cp "$ROOT/scripts/lib/linux-common.sh" "$bundle/scripts/lib/"
cp "$ROOT/deploy/agents/linux/docker-compose.yml" "$ROOT/deploy/agents/linux/config.alloy" \
  "$ROOT/deploy/agents/linux/config-cadvisor.alloy" "$ROOT/deploy/agents/linux/.env.example" "$bundle/deploy/agents/linux/"
cp "$ROOT/deploy/alloy/Dockerfile.patched" "$bundle/deploy/alloy/"
cp "$ROOT/deploy/alloy/patches/moby-cve-2026-34040.patch" "$bundle/deploy/alloy/patches/"
mkdir -p "$bundle/cert-source"
"$ROOT/scripts/bootstrap-pki.sh" issue-collector --ca-dir "$CA_DIR" --passphrase-file "$PASSPHRASE_FILE" \
  --organization "$ORGANIZATION" --name "$COLLECTOR_NAME" --days 30 --output-dir "$bundle/cert-source"

container_flag=""
[ "$WITH_CONTAINERS" = false ] || container_flag="--with-containers"
cat > "$bundle/install.sh" <<EOF
#!/bin/sh
set -eu
ROOT=\$(CDPATH='' cd -- "\$(dirname -- "\$0")" && pwd)
exec "\$ROOT/scripts/install-linux-collector.sh" --phase all --install-runtime --enable-service \\
  --host-name "$COLLECTOR_NAME" --environment "$ENVIRONMENT" --team "$TEAM" --location "$LOCATION" \\
  --metrics-endpoint "$GATEWAY_URL/api/v1/write" --logs-endpoint "$GATEWAY_URL/loki/api/v1/push" \\
  --otlp-endpoint "$GATEWAY_URL" --tls-server-name "$TLS_SERVER_NAME" \\
  --tls-ca-file "\$ROOT/cert-source/ca.crt" --tls-cert-file "\$ROOT/cert-source/client.crt" \\
  --tls-key-file "\$ROOT/cert-source/client.key" $container_flag "\$@"
EOF
chmod 700 "$bundle/install.sh" "$bundle/scripts/install-linux-collector.sh"
chmod 600 "$bundle/cert-source/client.key"
cat > "$bundle/README.md" <<EOF
# Collector SentinelOps: $COLLECTOR_NAME

1. Transfira o pacote por canal autenticado.
2. Confira o arquivo SHA-256 publicado junto ao pacote.
3. Extraia e execute sudo ./install.sh.
4. Confirme no dashboard Linux Hosts - Fleet e Detalhe.

Identidade: spiffe://sentinelops/organizations/$ORGANIZATION/collectors/$COLLECTOR_NAME
O certificado expira em 30 dias. Gere novo bundle antes da expiração.
EOF
archive="$OUTPUT_DIR/sentinelops-collector-$COLLECTOR_NAME.tar.gz"
tar -czf "$archive" -C "$stage" "sentinelops-collector-$COLLECTOR_NAME"
chmod 600 "$archive"
checksum=$(if command -v sha256sum >/dev/null 2>&1; then sha256sum "$archive" | awk '{print $1}'; else shasum -a 256 "$archive" | awk '{print $1}'; fi)
printf '%s  %s\n' "$checksum" "$(basename "$archive")" > "$archive.sha256"
chmod 600 "$archive.sha256"
printf 'Bundle: %s\nSHA-256: %s\n' "$archive" "$checksum"
