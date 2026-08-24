#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
SERVER_NAME=ingest.local
SERVER_IP=127.0.0.1
PKI_DIR="$ROOT/deploy/gateway/pki"
SECRET_DIR="$ROOT/.sentinelops/secrets"
PASSPHRASE_FILE="$SECRET_DIR/pki-ca-passphrase"

usage() {
  cat <<'EOF'
Uso: bootstrap-secure-ingest.sh [--server-name DNS] [--server-ip IP]

Cria uma CA local criptografada quando ela ainda não existe e emite/rotaciona o
certificado do gateway. A senha da CA fica em .sentinelops/secrets com modo
0600 e nunca é exibida. Em produção, substitua a CA local por PKI corporativa.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --server-name) SERVER_NAME=$2; shift 2 ;;
    --server-ip) SERVER_IP=$2; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Opção desconhecida: $1" >&2; exit 2 ;;
  esac
done
printf '%s' "$SERVER_NAME" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9.-]{1,252}$' || { echo "server-name inválido" >&2; exit 2; }
printf '%s' "$SERVER_IP" | grep -Eq '^[A-Fa-f0-9:.]+$' || { echo "server-ip inválido" >&2; exit 2; }
command -v openssl >/dev/null 2>&1 || { echo "openssl é obrigatório" >&2; exit 1; }

umask 077
mkdir -p "$PKI_DIR" "$SECRET_DIR"
if [ ! -s "$PASSPHRASE_FILE" ]; then
  [ ! -e "$PKI_DIR/ca.key" ] || { echo "CA existente sem passphrase local; importe a senha correta antes de rotacionar" >&2; exit 1; }
  openssl rand -base64 -out "$PASSPHRASE_FILE" 48
  chmod 600 "$PASSPHRASE_FILE"
fi
permissions=$(stat -c '%a' "$PASSPHRASE_FILE" 2>/dev/null || stat -f '%Lp' "$PASSPHRASE_FILE")
[ "$permissions" = 600 ] || { echo "passphrase da CA deve ter modo 0600" >&2; exit 1; }

if [ ! -s "$PKI_DIR/ca.key" ] || [ ! -s "$PKI_DIR/ca.crt" ]; then
  "$ROOT/scripts/bootstrap-pki.sh" init-ca --output-dir "$PKI_DIR" --passphrase-file "$PASSPHRASE_FILE"
else
  openssl pkey -in "$PKI_DIR/ca.key" -passin "file:$PASSPHRASE_FILE" -noout >/dev/null 2>&1 || {
    echo "passphrase local não abre a CA existente" >&2
    exit 1
  }
fi

staging=$(mktemp -d "$ROOT/.sentinelops/gateway-pki.XXXXXX")
cleanup() { find "$staging" -depth -delete; }
trap cleanup EXIT HUP INT TERM
"$ROOT/scripts/bootstrap-pki.sh" issue-server --ca-dir "$PKI_DIR" --passphrase-file "$PASSPHRASE_FILE" \
  --name "$SERVER_NAME" --dns-name "$SERVER_NAME" --ip "$SERVER_IP" --output-dir "$staging" --days 90
install -m 0600 "$staging/server.key" "$PKI_DIR/server.key"
install -m 0644 "$staging/server.crt" "$staging/client-ca.crt" "$PKI_DIR/"
echo "Gateway mTLS configurado para $SERVER_NAME ($SERVER_IP); CA preservada e certificado rotacionado."
