#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
IMAGE=${ALLOY_PATCHED_IMAGE:-sentinelops-alloy:1.18.1-patched.1}

docker build \
  --file "$ROOT/deploy/alloy/Dockerfile.patched" \
  --tag "$IMAGE" \
  "$ROOT"

docker run --rm --entrypoint /bin/alloy "$IMAGE" --version
echo "Imagem Alloy corrigida construída: $IMAGE"
