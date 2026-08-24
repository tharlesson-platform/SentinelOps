#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
MODE=""
OUTPUT_DIR="$ROOT/deploy/gateway/pki"
CA_DIR=""
PASSPHRASE_FILE=""
NAME=""
ORGANIZATION=""
DNS_NAME=""
IP_ADDRESS=""
DAYS=""

usage() {
  cat <<'EOF'
Uso:
  bootstrap-pki.sh init-ca --output-dir DIR --passphrase-file FILE
  bootstrap-pki.sh issue-server --ca-dir DIR --passphrase-file FILE --name NAME [--dns-name DNS] [--ip IP] --output-dir DIR
  bootstrap-pki.sh issue-collector --ca-dir DIR --passphrase-file FILE --organization ORG --name NAME --output-dir DIR

A chave da CA é AES-256 e a senha só é lida de arquivo modo 0600. Certificados
de servidor e collector usam chaves RSA 3072, SHA-256 e validade curta.
EOF
}

[ "$#" -gt 0 ] || { usage >&2; exit 2; }
MODE=$1
shift
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-dir) OUTPUT_DIR=$2; shift 2 ;;
    --ca-dir) CA_DIR=$2; shift 2 ;;
    --passphrase-file) PASSPHRASE_FILE=$2; shift 2 ;;
    --name) NAME=$2; shift 2 ;;
    --organization) ORGANIZATION=$2; shift 2 ;;
    --dns-name) DNS_NAME=$2; shift 2 ;;
    --ip) IP_ADDRESS=$2; shift 2 ;;
    --days) DAYS=$2; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'Opção desconhecida: %s\n' "$1" >&2; exit 2 ;;
  esac
done

command -v openssl >/dev/null 2>&1 || { echo "openssl é obrigatório" >&2; exit 1; }
[ -n "$PASSPHRASE_FILE" ] && [ -s "$PASSPHRASE_FILE" ] || { echo "--passphrase-file válido é obrigatório" >&2; exit 2; }
permissions=$(stat -c '%a' "$PASSPHRASE_FILE" 2>/dev/null || stat -f '%Lp' "$PASSPHRASE_FILE")
[ "$permissions" = 600 ] || { echo "passphrase-file deve ter modo 0600" >&2; exit 2; }

validate_name() {
  printf '%s' "$1" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._-]{1,62}$' || { echo "nome inválido: $1" >&2; exit 2; }
}

make_key() {
  destination=$1
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$destination" >/dev/null 2>&1
  chmod 600 "$destination"
}

sign_request() {
  request=$1
  certificate=$2
  extension_file=$3
  validity=$4
  openssl x509 -req -sha256 -days "$validity" -in "$request" \
    -CA "$CA_DIR/ca.crt" -CAkey "$CA_DIR/ca.key" -passin "file:$PASSPHRASE_FILE" \
    -CAserial "$CA_DIR/ca.srl" -CAcreateserial -extfile "$extension_file" -out "$certificate" >/dev/null 2>&1
}

umask 077
mkdir -p "$OUTPUT_DIR"
case "$MODE" in
  init-ca)
    [ ! -e "$OUTPUT_DIR/ca.key" ] || { echo "CA existente; recusa sobrescrever $OUTPUT_DIR/ca.key" >&2; exit 1; }
    openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 -aes-256-cbc \
      -pass "file:$PASSPHRASE_FILE" -out "$OUTPUT_DIR/ca.key" >/dev/null 2>&1
    openssl req -x509 -new -sha256 -days "${DAYS:-3650}" -key "$OUTPUT_DIR/ca.key" \
      -passin "file:$PASSPHRASE_FILE" -subj '/CN=SentinelOps Collector Root CA/O=SentinelOps' -out "$OUTPUT_DIR/ca.crt"
    chmod 600 "$OUTPUT_DIR/ca.key"
    chmod 644 "$OUTPUT_DIR/ca.crt"
    ;;
  issue-server)
    [ -n "$CA_DIR" ] && [ -s "$CA_DIR/ca.key" ] && [ -s "$CA_DIR/ca.crt" ] || { echo "--ca-dir inválido" >&2; exit 2; }
    validate_name "$NAME"
    [ -n "$DNS_NAME" ] || DNS_NAME=$NAME
    validate_name "$DNS_NAME"
    make_key "$OUTPUT_DIR/server.key"
    request=$(mktemp)
    extensions=$(mktemp)
    trap 'rm -f "$request" "$extensions"' EXIT HUP INT TERM
    openssl req -new -sha256 -key "$OUTPUT_DIR/server.key" -subj "/CN=$DNS_NAME/O=SentinelOps" -out "$request"
    san="DNS:$DNS_NAME"
    [ -z "$IP_ADDRESS" ] || san="$san,IP:$IP_ADDRESS"
    printf 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\nsubjectAltName=%s\n' "$san" > "$extensions"
    sign_request "$request" "$OUTPUT_DIR/server.crt" "$extensions" "${DAYS:-90}"
    cp "$CA_DIR/ca.crt" "$OUTPUT_DIR/client-ca.crt"
    chmod 644 "$OUTPUT_DIR/server.crt" "$OUTPUT_DIR/client-ca.crt"
    ;;
  issue-collector)
    [ -n "$CA_DIR" ] && [ -s "$CA_DIR/ca.key" ] && [ -s "$CA_DIR/ca.crt" ] || { echo "--ca-dir inválido" >&2; exit 2; }
    validate_name "$NAME"
    validate_name "$ORGANIZATION"
    make_key "$OUTPUT_DIR/client.key"
    request=$(mktemp)
    extensions=$(mktemp)
    trap 'rm -f "$request" "$extensions"' EXIT HUP INT TERM
    openssl req -new -sha256 -key "$OUTPUT_DIR/client.key" -subj "/CN=$NAME/O=SentinelOps Collectors" -out "$request"
    printf 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=clientAuth\nsubjectAltName=URI:spiffe://sentinelops/organizations/%s/collectors/%s\n' "$ORGANIZATION" "$NAME" > "$extensions"
    sign_request "$request" "$OUTPUT_DIR/client.crt" "$extensions" "${DAYS:-30}"
    cp "$CA_DIR/ca.crt" "$OUTPUT_DIR/ca.crt"
    chmod 644 "$OUTPUT_DIR/client.crt" "$OUTPUT_DIR/ca.crt"
    ;;
  *) usage >&2; exit 2 ;;
esac

echo "PKI gerada em $OUTPUT_DIR sem exibir material secreto."
