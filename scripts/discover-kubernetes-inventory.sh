#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
CONFIG=""
OUTPUT=""

usage() {
  printf '%s\n' 'Uso: ./scripts/discover-kubernetes-inventory.sh --config ESCOPO.json --output INVENTORY.json'
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) CONFIG=${2:?}; shift 2 ;;
    --output) OUTPUT=${2:?}; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'erro: opção desconhecida: %s\n' "$1" >&2; exit 2 ;;
  esac
done

[ -n "$CONFIG" ] && [ -n "$OUTPUT" ] || { usage >&2; exit 2; }
exec go run "$ROOT/apps/kubeinventory" --config "$CONFIG" --output "$OUTPUT"
