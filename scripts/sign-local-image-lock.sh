#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
LOCK_FILE=${SENTINEL_IMAGE_LOCK_FILE:-$ROOT/artifacts/runtime/docker-compose.images.lock.yml}
COSIGN_IMAGE=ghcr.io/sigstore/cosign/cosign:v3.0.5@sha256:be924970ba7438c22e18067dec5637946d6566eac711f5bedd1584e7137008fb

for dependency in docker jq openssl; do
  command -v "$dependency" >/dev/null 2>&1 || { echo "Dependencia ausente: $dependency" >&2; exit 2; }
done
test -f "$LOCK_FILE" || { echo "Lock de imagens ausente: execute make prepare-images." >&2; exit 2; }

umask 077
evidence_dir="$ROOT/artifacts/release/local-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$evidence_dir"
chmod 700 "$evidence_dir"
work_dir=$(mktemp -d "$evidence_dir/.signing.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

images_json='[]'
for entry in \
  api=sentinelops-api:0.1.0-local \
  worker=sentinelops-worker:0.1.0-local \
  migrate=sentinelops-migrate:0.1.0-local \
  agent=sentinelops-agent:0.1.0-local \
  web=sentinelops-web:0.1.0-local \
  demo=sentinelops-demo-api:0.2.0-local \
  alloy=sentinelops-alloy:1.18.1-patched.2 \
  caddy=sentinelops-caddy:2.11.4-patched.1
do
  name=${entry%%=*}
  image=${entry#*=}
  image_id=$(docker image inspect "$image" --format '{{.Id}}')
  images_json=$(printf '%s' "$images_json" | jq --arg name "$name" --arg source "$image" --arg imageId "$image_id" '. + [{name:$name,source:$source,imageId:$imageId}]')
done

source_dirty=false
[ -z "$(git -C "$ROOT" status --porcelain=v1 --untracked-files=all)" ] || source_dirty=true

jq -n \
  --arg generatedAt "$(date -u +%FT%TZ)" \
  --arg sourceRevision "$(git -C "$ROOT" rev-parse HEAD)" \
  --argjson sourceDirty "$source_dirty" \
  --arg lockSHA256 "$(openssl dgst -sha256 "$LOCK_FILE" | awk '{print $NF}')" \
  --argjson images "$images_json" \
  '{schemaVersion:1,generatedAt:$generatedAt,sourceRevision:$sourceRevision,sourceDirty:$sourceDirty,imageLockSHA256:$lockSHA256,images:$images}' \
  > "$work_dir/manifest.json"

COSIGN_PASSWORD=$(openssl rand -hex 32)
export COSIGN_PASSWORD
docker run --rm -e COSIGN_PASSWORD -v "$work_dir:/work" "$COSIGN_IMAGE" \
  generate-key-pair --output-key-prefix /work/local-signing >/dev/null
docker run --rm -e COSIGN_PASSWORD -v "$work_dir:/work" "$COSIGN_IMAGE" \
  sign-blob --yes --key /work/local-signing.key --bundle /work/manifest.bundle.json /work/manifest.json >/dev/null
docker run --rm -v "$work_dir:/work" "$COSIGN_IMAGE" \
  verify-blob --key /work/local-signing.pub --bundle /work/manifest.bundle.json /work/manifest.json >/dev/null

cp "$work_dir/manifest.json" "$evidence_dir/manifest.json"
cp "$work_dir/manifest.bundle.json" "$evidence_dir/manifest.bundle.json"
cp "$work_dir/local-signing.pub" "$evidence_dir/local-signing.pub"
chmod 600 "$evidence_dir"/*
rm -rf "$work_dir"
trap - EXIT HUP INT TERM

printf 'immutable_manifest=PASS\nsignature=PASS\nverification=PASS\nprivate_key_retained=false\nevidence=%s\n' "$evidence_dir"
