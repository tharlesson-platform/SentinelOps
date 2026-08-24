#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
API_URL=${SENTINEL_API_URL:-http://127.0.0.1:8080}
DEMO_URL=${SENTINEL_DEMO_URL:-http://127.0.0.1:8090}
PROMETHEUS_URL=${SENTINEL_PROMETHEUS_URL:-http://127.0.0.1:9090}
LOKI_URL=${SENTINEL_LOKI_URL:-http://127.0.0.1:3100}
TEMPO_URL=${SENTINEL_TEMPO_URL:-http://127.0.0.1:3200}
PYROSCOPE_URL=${SENTINEL_PYROSCOPE_URL:-http://127.0.0.1:4040}
ALLOY_URL=${SENTINEL_ALLOY_URL:-http://127.0.0.1:12345}

for dependency in curl jq docker; do
  command -v "$dependency" >/dev/null 2>&1 || { echo "Dependência ausente: $dependency" >&2; exit 2; }
done
test -f "$ROOT/.env" || { echo "Execute make bootstrap antes da prova." >&2; exit 2; }
# shellcheck disable=SC1091
. "$ROOT/.env"

wait_http() {
  wait_name=$1
  wait_url=$2
  wait_limit=${3:-30}
  wait_attempt=0
  while [ "$wait_attempt" -lt "$wait_limit" ]; do
    if curl --max-time 4 -fsS "$wait_url" >/dev/null 2>&1; then
      return 0
    fi
    wait_attempt=$((wait_attempt + 1))
    sleep 2
  done
  echo "Timeout aguardando $wait_name em $wait_url" >&2
  return 1
}

has_expected_services() {
  jq -e '
    ([.[]] | unique | length) >= 3 and
    (index("sentinel-demo-api") != null) and
    (index("sentinel-demo-orders") != null) and
    (index("sentinel-demo-payments") != null)
  ' >/dev/null
}

wait_http api "$API_URL/readyz"
wait_http demo "$DEMO_URL/ready"
wait_http alloy "$ALLOY_URL/-/ready"
wait_http prometheus "$PROMETHEUS_URL/-/ready"
wait_http loki "$LOKI_URL/ready"
wait_http tempo "$TEMPO_URL/ready"
# Pyroscope single-binary promotes metastore, ingester and segment-writer
# readiness sequentially after a cold start; retained volumes can extend this
# beyond one minute while recovery/compaction completes.
wait_http pyroscope "$PYROSCOPE_URL/ready" 120

proof_started_at=$(date -u +%FT%TZ)
proof_marker="local-e2e-$(date -u +%Y%m%dT%H%M%SZ)-$$"
chain_response=$(curl --max-time 15 -fsS "$DEMO_URL/api/checkout" \
  -H "X-Demo-Marker: $proof_marker" \
  -H 'X-Synthetic-Test: sentinelops')
trace_id=$(printf '%s' "$chain_response" | jq -er .traceId)
printf '%s' "$chain_response" | jq -e --arg marker "$proof_marker" --arg trace "$trace_id" '
  .service == "sentinel-demo-api" and .marker == $marker and .traceId == $trace and
  .downstream.service == "sentinel-demo-orders" and .downstream.marker == $marker and .downstream.traceId == $trace and
  .downstream.downstream.service == "sentinel-demo-payments" and
  .downstream.downstream.marker == $marker and .downstream.downstream.traceId == $trace
' >/dev/null

metric_services='[]'
metric_up='0'
metric_attempt=0
while [ "$metric_attempt" -lt 30 ]; do
  metric_response=$(curl --max-time 5 -fsSG "$PROMETHEUS_URL/api/v1/query" \
    --data-urlencode 'query=count by (service) (demo_pipeline_requests_total{job="sentinel-demo-apps"})')
  metric_services=$(printf '%s' "$metric_response" | jq '[.data.result[].metric.service] | unique')
  metric_up=$(curl --max-time 5 -fsSG "$PROMETHEUS_URL/api/v1/query" \
    --data-urlencode 'query=min(up{job="sentinel-demo-apps"})' | jq -r '.data.result[0].value[1] // "0"')
  if printf '%s' "$metric_services" | has_expected_services && [ "$metric_up" = 1 ]; then
    break
  fi
  metric_attempt=$((metric_attempt + 1))
  sleep 2
done
printf '%s' "$metric_services" | has_expected_services || { echo "Métricas dos três mocks não chegaram via Alloy." >&2; exit 1; }
[ "$metric_up" = 1 ] || { echo "Ao menos um alvo de métricas está indisponível." >&2; exit 1; }

log_services='[]'
log_attempt=0
while [ "$log_attempt" -lt 30 ]; do
  log_response=$(curl --max-time 5 -fsSG "$LOKI_URL/loki/api/v1/query_range" \
    --data-urlencode "query={service_namespace=\"sentinel-demo\"} | marker = \"$proof_marker\"" \
    --data-urlencode 'limit=50')
  log_services=$(printf '%s' "$log_response" | jq '[.data.result[].stream.service_name] | unique')
  if printf '%s' "$log_services" | has_expected_services; then
    break
  fi
  log_attempt=$((log_attempt + 1))
  sleep 2
done
printf '%s' "$log_services" | has_expected_services || { echo "Logs correlacionados dos três mocks não chegaram ao Loki." >&2; exit 1; }

trace_services='[]'
trace_attempt=0
while [ "$trace_attempt" -lt 30 ]; do
  trace_response=$(curl --max-time 5 -fsS "$TEMPO_URL/api/traces/$trace_id" 2>/dev/null || printf '{}')
  trace_services=$(printf '%s' "$trace_response" | jq '[.batches[]?.resource.attributes[]? | select(.key=="service.name") | .value.stringValue] | unique')
  if printf '%s' "$trace_services" | has_expected_services; then
    break
  fi
  trace_attempt=$((trace_attempt + 1))
  sleep 2
done
printf '%s' "$trace_services" | has_expected_services || { echo "Trace distribuído completo não chegou ao Tempo." >&2; exit 1; }

profile_services='[]'
profile_attempt=0
while [ "$profile_attempt" -lt 30 ]; do
  profile_now=$(date +%s)
  profile_response=$(curl --max-time 5 -fsS -X POST "$PYROSCOPE_URL/querier.v1.QuerierService/LabelValues" \
    -H 'Content-Type: application/json' \
    --data "{\"start\":\"$((profile_now-900))000\",\"end\":\"${profile_now}000\",\"name\":\"service_name\"}")
  profile_services=$(printf '%s' "$profile_response" | jq '[.names[] | select(startswith("sentinel-demo-"))] | unique')
  if printf '%s' "$profile_services" | has_expected_services; then
    break
  fi
  profile_attempt=$((profile_attempt + 1))
  sleep 3
done
printf '%s' "$profile_services" | has_expected_services || { echo "Perfis dos três mocks não chegaram ao Pyroscope." >&2; exit 1; }
profile_ticks=$(curl --max-time 20 -fsSG "$PYROSCOPE_URL/pyroscope/render" \
  --data-urlencode 'query=process_cpu:cpu:nanoseconds:cpu:nanoseconds{service_name="sentinel-demo-api"}' \
  --data-urlencode 'from=now-15m' | jq -er '.flamebearer.numTicks')
[ "$profile_ticks" -gt 0 ] || { echo "Perfil CPU não contém amostras." >&2; exit 1; }

"$ROOT/scripts/seed.sh" >/dev/null
login_payload=$(jq -nc --arg username "$LOCAL_ADMIN_USER" --arg password "$LOCAL_ADMIN_PASSWORD" '{username:$username,password:$password}')
access_token=$(curl --max-time 5 -fsS -X POST "$API_URL/api/v1/auth/login" -H 'Content-Type: application/json' --data "$login_payload" | jq -er .data.accessToken)
catalog_services=$(curl --max-time 5 -fsS "$API_URL/api/v1/services" -H "Authorization: Bearer $access_token" | jq '[.data[].name] | unique')
printf '%s' "$catalog_services" | has_expected_services || { echo "Catálogo do control plane não contém os três mocks." >&2; exit 1; }

gate_output=$("$ROOT/scripts/prove-observational-gates.sh")
standard_result=$(printf '%s\n' "$gate_output" | awk -F= '$1=="standard_pass_result" {print $2}')
missing_policy_result=$(printf '%s\n' "$gate_output" | awk -F= '$1=="missing_policy_result" {print $2}')
over_threshold_result=$(printf '%s\n' "$gate_output" | awk -F= '$1=="over_threshold_result" {print $2}')
if [ "$standard_result" != PASS ] || [ "$missing_policy_result" != INCONCLUSIVE ] || [ "$over_threshold_result" != FAIL ]; then
  echo "Quality gates não comprovaram PASS/INCONCLUSIVE/FAIL." >&2
  exit 1
fi

evidence_dir="$ROOT/artifacts/local-e2e"
mkdir -p "$evidence_dir"
evidence_file="$evidence_dir/$(date -u +%Y%m%dT%H%M%SZ).json"
umask 077
jq -n \
  --arg startedAt "$proof_started_at" \
  --arg completedAt "$(date -u +%FT%TZ)" \
  --arg marker "$proof_marker" \
  --arg traceId "$trace_id" \
  --argjson metricServices "$metric_services" \
  --argjson logServices "$log_services" \
  --argjson traceServices "$trace_services" \
  --argjson profileServices "$profile_services" \
  --argjson catalogServices "$catalog_services" \
  --arg standardResult "$standard_result" \
  --arg missingPolicyResult "$missing_policy_result" \
  --arg overThresholdResult "$over_threshold_result" \
  --arg profileTicks "$profile_ticks" \
  '{schemaVersion:1,startedAt:$startedAt,completedAt:$completedAt,marker:$marker,traceId:$traceId,
    collection:{metrics:{path:"mock -> Alloy scrape -> Prometheus remote write",services:$metricServices,allTargetsUp:true},
      logs:{path:"mock OTLP -> Alloy processors -> Loki",services:$logServices},
      traces:{path:"mock OTLP -> Alloy tail sampling -> Tempo",services:$traceServices},
      profiles:{path:"Alloy pprof scrape -> Pyroscope",services:$profileServices,cpuTicks:($profileTicks|tonumber)}},
    processing:{catalogServices:$catalogServices,qualityGates:{standard:$standardResult,missingPolicy:$missingPolicyResult,overThreshold:$overThresholdResult}}}' \
  > "$evidence_file"
chmod 600 "$evidence_file"

printf 'mock_chain=PASS\ntrace_id=%s\nmetric_services=3\nlog_services=3\ntrace_services=3\nprofile_services=3\nquality_gate_standard=%s\nquality_gate_missing_policy=%s\nquality_gate_over_threshold=%s\nevidence=%s\n' \
  "$trace_id" "$standard_result" "$missing_policy_result" "$over_threshold_result" "$evidence_file"
