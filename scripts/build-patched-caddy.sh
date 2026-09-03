#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
IMAGE=${SENTINEL_CADDY_IMAGE:-sentinelops-caddy:2.11.4-patched.1}

command -v docker >/dev/null 2>&1 || { echo "Docker e obrigatorio." >&2; exit 2; }

docker build \
  --file "$ROOT/deploy/caddy/Dockerfile.patched" \
  --tag "$IMAGE" \
  "$ROOT"

printf 'image=%s\n' "$IMAGE"
