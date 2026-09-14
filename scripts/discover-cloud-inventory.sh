#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
CONFIG=""
OUTPUT=""

usage() {
  cat <<'EOF'
Uso: ./scripts/discover-cloud-inventory.sh --config ESCOPO.json --output INVENTORY.json

Executa somente consultas read-only por Azure CLI ou AWS CLI, conforme o
provider do arquivo de escopo. Cada arquivo representa uma única
subscription Azure ou uma única account/região AWS previamente aprovada.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) CONFIG=${2:?}; shift 2 ;;
    --output) OUTPUT=${2:?}; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'erro: opção desconhecida: %s\n' "$1" >&2; exit 2 ;;
  esac
done

if [ -z "$CONFIG" ] || [ -z "$OUTPUT" ]; then
  usage >&2
  exit 2
fi
exec go run "$ROOT/apps/cloudinventory" --config "$CONFIG" --output "$OUTPUT"
