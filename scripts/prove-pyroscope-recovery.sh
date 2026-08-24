#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
LOCK_FILE=${SENTINEL_IMAGE_LOCK_FILE:-$ROOT/artifacts/runtime/docker-compose.images.lock.yml}
PYROSCOPE_URL=${SENTINEL_PYROSCOPE_URL:-http://127.0.0.1:4040}
STARTUP_SLO_SECONDS=${SENTINEL_PYROSCOPE_STARTUP_SLO_SECONDS:-240}

for dependency in curl docker jq; do
  command -v "$dependency" >/dev/null 2>&1 || { echo "Dependencia ausente: $dependency" >&2; exit 2; }
done
test -f "$LOCK_FILE" || { echo "Lock de imagens ausente: execute make prepare-images." >&2; exit 2; }

compose() {
  docker compose --env-file "$ROOT/.env" \
    -f "$ROOT/deploy/compose/docker-compose.yml" \
    -f "$ROOT/deploy/compose/docker-compose.ha.yml" \
    -f "$LOCK_FILE" "$@"
}

has_demo_profiles() {
  now=$(date +%s)
  curl --max-time 10 -fsS -X POST "$PYROSCOPE_URL/querier.v1.QuerierService/LabelValues" \
    -H 'Content-Type: application/json' \
    --data "{\"start\":\"$((now-3600))000\",\"end\":\"${now}000\",\"name\":\"service_name\"}" |
    jq -e '([.names[] | select(startswith("sentinel-demo-"))] | unique | length) >= 3' >/dev/null
}

wait_ready() {
  wait_started=$(date +%s)
  while ! curl --max-time 4 -fsS "$PYROSCOPE_URL/ready" >/dev/null 2>&1; do
    wait_elapsed=$(($(date +%s) - wait_started))
    [ "$wait_elapsed" -lt "$STARTUP_SLO_SECONDS" ] || { echo "Pyroscope nao ficou pronto dentro de ${STARTUP_SLO_SECONDS}s." >&2; exit 1; }
    sleep 2
  done
}

wait_profiles() {
  profile_attempt=0
  while ! has_demo_profiles; do
    profile_attempt=$((profile_attempt + 1))
    [ "$profile_attempt" -lt 30 ] || { echo "Perfis retidos nao ficaram consultaveis." >&2; exit 1; }
    sleep 2
  done
}

wait_ready
wait_profiles

pyroscope_id=$(compose ps -q pyroscope)
readiness_id=$(compose ps -q pyroscope-readiness)
test -n "$pyroscope_id" && test -n "$readiness_id"

attempt=0
while [ "$attempt" -lt 30 ]; do
  readiness_state=$(docker inspect "$readiness_id" --format '{{.State.Health.Status}}')
  [ "$readiness_state" = healthy ] && break
  attempt=$((attempt + 1))
  sleep 2
done
[ "$readiness_state" = healthy ] || { echo "Guard de readiness nao ficou healthy antes do teste." >&2; exit 1; }

docker stop --time 10 "$pyroscope_id" >/dev/null
restore_pyroscope() { compose up -d --no-deps pyroscope >/dev/null 2>&1 || true; }
trap restore_pyroscope EXIT HUP INT TERM
attempt=0
while [ "$attempt" -lt 25 ]; do
  readiness_state=$(docker inspect "$readiness_id" --format '{{.State.Health.Status}}')
  [ "$readiness_state" = unhealthy ] && break
  attempt=$((attempt + 1))
  sleep 1
done
[ "${readiness_state:-unknown}" = unhealthy ] || { echo "Guard de readiness nao detectou Pyroscope indisponivel." >&2; exit 1; }

started_epoch=$(date +%s)
compose up -d --no-deps pyroscope >/dev/null
while ! curl --max-time 4 -fsS "$PYROSCOPE_URL/ready" >/dev/null 2>&1; do
  elapsed=$(($(date +%s) - started_epoch))
  [ "$elapsed" -lt "$STARTUP_SLO_SECONDS" ] || { echo "Pyroscope excedeu SLO de startup de ${STARTUP_SLO_SECONDS}s." >&2; exit 1; }
  sleep 2
done
startup_seconds=$(($(date +%s) - started_epoch))

attempt=0
while [ "$attempt" -lt 20 ]; do
  readiness_state=$(docker inspect "$readiness_id" --format '{{.State.Health.Status}}')
  [ "$readiness_state" = healthy ] && break
  attempt=$((attempt + 1))
  sleep 2
done
[ "$readiness_state" = healthy ] || { echo "Guard de readiness nao recuperou estado healthy." >&2; exit 1; }
wait_profiles
trap - EXIT HUP INT TERM

evidence_dir="$ROOT/artifacts/resilience"
mkdir -p "$evidence_dir"
evidence_file="$evidence_dir/pyroscope-recovery-$(date -u +%Y%m%dT%H%M%SZ).json"
umask 077
jq -n \
  --arg completedAt "$(date -u +%FT%TZ)" \
  --argjson startupSeconds "$startup_seconds" \
  --argjson startupSLOSeconds "$STARTUP_SLO_SECONDS" \
  '{schemaVersion:1,completedAt:$completedAt,test:"retained-volume restart",readinessGuardDetectedFailure:true,startupSeconds:$startupSeconds,startupSLOSeconds:$startupSLOSeconds,readinessRecovered:true,retainedProfilesVerified:true}' \
  > "$evidence_file"
chmod 600 "$evidence_file"

printf 'readiness_failure_detection=PASS\nstartup_seconds=%s\nstartup_slo_seconds=%s\nreadiness_recovery=PASS\nretained_profiles=PASS\nevidence=%s\n' \
  "$startup_seconds" "$STARTUP_SLO_SECONDS" "$evidence_file"
