#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
OUTPUT=${SENTINEL_IMAGE_LOCK_FILE:-$ROOT/artifacts/runtime/docker-compose.images.lock.yml}

for dependency in docker awk; do
  command -v "$dependency" >/dev/null 2>&1 || { echo "Dependencia ausente: $dependency" >&2; exit 2; }
done

image_id() {
  image_name=$1
  image_value=$(docker image inspect "$image_name" --format '{{.Id}}' 2>/dev/null || true)
  case "$image_value" in
    sha256:????????????????????????????????????????????????????????????????) printf '%s' "$image_value" ;;
    *) echo "Imagem local ausente ou sem ID SHA256 valido: $image_name" >&2; exit 1 ;;
  esac
}

api_id=$(image_id sentinelops-api:0.1.0-local)
worker_id=$(image_id sentinelops-worker:0.1.0-local)
migrate_id=$(image_id sentinelops-migrate:0.1.0-local)
agent_id=$(image_id sentinelops-agent:0.1.0-local)
web_id=$(image_id sentinelops-web:0.1.0-local)
demo_id=$(image_id sentinelops-demo-api:0.2.0-local)
alloy_id=$(image_id sentinelops-alloy:1.18.1-patched.2)
caddy_id=$(image_id sentinelops-caddy:2.11.4-patched.1)

mkdir -p "$(dirname -- "$OUTPUT")"
temporary=$(mktemp "${OUTPUT}.XXXXXX")
trap 'rm -f "$temporary"' EXIT HUP INT TERM
umask 077
{
  printf 'services:\n'
  for service in api loki-init; do printf '  %s: { image: "%s", pull_policy: never }\n' "$service" "$api_id"; done
  printf '  worker: { image: "%s", pull_policy: never }\n' "$worker_id"
  printf '  migrate: { image: "%s", pull_policy: never }\n' "$migrate_id"
  printf '  agent: { image: "%s", pull_policy: never }\n' "$agent_id"
  for service in web api-edge; do printf '  %s: { image: "%s", pull_policy: never }\n' "$service" "$web_id"; done
  for service in demo-api demo-orders demo-payments demo-traffic; do printf '  %s: { image: "%s", pull_policy: never }\n' "$service" "$demo_id"; done
  printf '  alloy: { image: "%s", pull_policy: never }\n' "$alloy_id"
  printf '  ingest-gateway: { image: "%s", pull_policy: never }\n' "$caddy_id"
} > "$temporary"
chmod 600 "$temporary"
mv "$temporary" "$OUTPUT"
trap - EXIT HUP INT TERM

printf 'image_lock=%s\napi=%s\nworker=%s\nweb=%s\ndemo=%s\nalloy=%s\ncaddy=%s\n' \
  "$OUTPUT" "$api_id" "$worker_id" "$web_id" "$demo_id" "$alloy_id" "$caddy_id"
