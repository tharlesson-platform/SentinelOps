#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
LOCK_FILE=${SENTINEL_IMAGE_LOCK_FILE:-$ROOT/artifacts/runtime/docker-compose.images.lock.yml}
API_URL=${SENTINEL_API_URL:-http://127.0.0.1:8080}
PROBE_COUNT=${SENTINEL_HA_PROBE_COUNT:-50}

for dependency in curl docker jq; do
  command -v "$dependency" >/dev/null 2>&1 || { echo "Dependencia ausente: $dependency" >&2; exit 2; }
done
test -f "$ROOT/.env" || { echo "Execute make bootstrap primeiro." >&2; exit 2; }
test -f "$LOCK_FILE" || { echo "Lock de imagens ausente: execute make prepare-images." >&2; exit 2; }

compose() {
  docker compose --env-file "$ROOT/.env" \
    -f "$ROOT/deploy/compose/docker-compose.yml" \
    -f "$ROOT/deploy/compose/docker-compose.ha.yml" \
    -f "$LOCK_FILE" "$@"
}

wait_replicas() {
  expected=$1
  attempt=0
  while [ "$attempt" -lt 60 ]; do
    running=$(compose ps --status running -q api | awk 'NF {count++} END {print count+0}')
    healthy=0
    for container_id in $(compose ps --status running -q api); do
      state=$(docker inspect "$container_id" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}')
      [ "$state" = healthy ] && healthy=$((healthy + 1))
    done
    [ "$running" -eq "$expected" ] && [ "$healthy" -eq "$expected" ] && return 0
    attempt=$((attempt + 1))
    sleep 2
  done
  echo "As $expected replicas de API nao ficaram saudaveis." >&2
  return 1
}

restore_replicas() {
  compose up -d --no-build --scale api=2 api >/dev/null 2>&1 || true
}

wait_replicas 2
worker_attempt=0
while [ "$worker_attempt" -lt 60 ]; do
  worker_running=$(compose ps --status running -q worker | awk 'NF {count++} END {print count+0}')
  worker_healthy=0
  for container_id in $(compose ps --status running -q worker); do
    state=$(docker inspect "$container_id" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}')
    [ "$state" = healthy ] && worker_healthy=$((worker_healthy + 1))
  done
  [ "$worker_running" -eq 2 ] && [ "$worker_healthy" -eq 2 ] && break
  worker_attempt=$((worker_attempt + 1))
  sleep 2
done
[ "$worker_running" -eq 2 ] && [ "$worker_healthy" -eq 2 ] || { echo "As duas replicas de worker nao ficaram saudaveis." >&2; exit 1; }
curl --max-time 5 -fsS "$API_URL/readyz" >/dev/null

for service in api worker web demo-api alloy api-edge; do
  for container_id in $(compose ps -q "$service"); do
    configured_image=$(docker inspect "$container_id" --format '{{.Config.Image}}')
    case "$configured_image" in
      sha256:????????????????????????????????????????????????????????????????) ;;
      *) echo "Servico $service nao usa ID imutavel: $configured_image" >&2; exit 1 ;;
    esac
  done
done

victim=$(compose ps --status running -q api | awk 'NR==1 {print; exit}')
test -n "$victim" || { echo "Replica para failover nao encontrada." >&2; exit 1; }
trap restore_replicas EXIT HUP INT TERM
docker stop --time 2 "$victim" >/dev/null

failover_started=$(date +%s)
attempt=0
consecutive_success=0
while [ "$consecutive_success" -lt 10 ]; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 30 ] || { echo "API nao estabilizou apos interrupcao de uma replica." >&2; exit 1; }
  if curl --max-time 2 -fsS "$API_URL/readyz" >/dev/null 2>&1; then
    consecutive_success=$((consecutive_success + 1))
  else
    consecutive_success=0
  fi
  sleep 1
done
convergence_seconds=$(($(date +%s) - failover_started))

probe=0
while [ "$probe" -lt "$PROBE_COUNT" ]; do
  curl --max-time 3 -fsS "$API_URL/readyz" >/dev/null
  probe=$((probe + 1))
done

restore_replicas
wait_replicas 2
trap - EXIT HUP INT TERM

evidence_dir="$ROOT/artifacts/resilience"
mkdir -p "$evidence_dir"
evidence_file="$evidence_dir/ha-$(date -u +%Y%m%dT%H%M%SZ).json"
umask 077
jq -n \
  --arg completedAt "$(date -u +%FT%TZ)" \
  --arg victim "$victim" \
  --argjson replicas 2 \
  --argjson successfulProbes "$PROBE_COUNT" \
  --argjson convergenceSeconds "$convergence_seconds" \
  '{schemaVersion:1,completedAt:$completedAt,scope:"local process HA",apiReplicas:$replicas,workerReplicas:2,stoppedReplica:$victim,failover:{successfulProbes:$successfulProbes,convergenceSeconds:$convergenceSeconds},restoredReplicas:2,immutableContainerImages:true}' \
  > "$evidence_file"
chmod 600 "$evidence_file"

printf 'api_replicas=2\nworker_replicas=2\nfailover=PASS\nsuccessful_probes=%s\nconvergence_seconds=%s\nreplicas_restored=2\nimmutable_container_images=PASS\nevidence=%s\n' \
  "$PROBE_COUNT" "$convergence_seconds" "$evidence_file"
