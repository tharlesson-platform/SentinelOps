#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
LANGUAGE=$(printenv LANGUAGE 2>/dev/null || printf go)
SERVICE_NAME=$(printenv SERVICE_NAME 2>/dev/null || printf sentinel-bootstrap-proof)
ENVIRONMENT=$(printenv ENVIRONMENT 2>/dev/null || printf development)
OTLP_ENDPOINT=$(printenv OTLP_ENDPOINT 2>/dev/null || printf http://127.0.0.1:4318)
OUTPUT_ROOT="$ROOT/artifacts/onboarding-proof"
PROMETHEUS_URL=$(printenv PROMETHEUS_URL 2>/dev/null || printf http://127.0.0.1:9090)
LOKI_URL=$(printenv LOKI_URL 2>/dev/null || printf http://127.0.0.1:3100)
TEMPO_URL=$(printenv TEMPO_URL 2>/dev/null || printf http://127.0.0.1:3200)

"$ROOT/scripts/bootstrap-apm.sh" --language "$LANGUAGE" --service-name "$SERVICE_NAME" \
  --environment "$ENVIRONMENT" --otlp-endpoint "$OTLP_ENDPOINT" --output "$OUTPUT_ROOT" --force --verify "$@"

verification="$OUTPUT_ROOT/$SERVICE_NAME/verification.json"
marker=$(jq -r .marker "$verification")
trace_id=$(jq -r .traceId "$verification")

wait_json() {
  description=$1
  shift
  attempts=0
  while [ "$attempts" -lt 30 ]; do
    response=$("$@" 2>/dev/null || true)
    if printf '%s' "$response" | jq -e '.status == "success" and (.data.result | length) > 0' >/dev/null 2>&1; then return 0; fi
    attempts=$((attempts + 1))
    sleep 2
  done
  printf '%s\n' "[sentinelops][erro] não comprovado no backend: $description" >&2
  exit 1
}

wait_json metric curl -fsS --get --data-urlencode "query=sentinelops_apm_bootstrap{job=\"$SERVICE_NAME\"} or sentinelops_apm_bootstrap{service_name=\"$SERVICE_NAME\"}" "$PROMETHEUS_URL/api/v1/query"
wait_json log curl -fsS --get --data-urlencode "query={service_name=\"$SERVICE_NAME\"} |= \"$marker\"" "$LOKI_URL/loki/api/v1/query_range"

attempts=0
until curl -fsS --max-time 10 "$TEMPO_URL/api/traces/$trace_id" >/dev/null 2>&1; do
  attempts=$((attempts + 1))
  [ "$attempts" -lt 30 ] || { echo "[sentinelops][erro] trace não persistido no Tempo" >&2; exit 1; }
  sleep 2
done

artifact="$OUTPUT_ROOT/$SERVICE_NAME/storage-proof.json"
jq -n --arg provedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg marker "$marker" --arg traceId "$trace_id" \
  --arg service "$SERVICE_NAME" '{provedAt:$provedAt,service:$service,marker:$marker,traceId:$traceId,backends:{metrics:"prometheus",logs:"loki",traces:"tempo"},status:"PASS"}' > "$artifact"
chmod 600 "$artifact"
printf 'Onboarding APM comprovado até a persistência: %s\n' "$artifact"
