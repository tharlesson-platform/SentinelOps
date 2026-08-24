#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
API_URL=${SENTINEL_API_URL:-http://127.0.0.1:8080}
# shellcheck disable=SC1091
. "$ROOT/.env"
mkdir -p "$ROOT/artifacts/validation"

login_payload=$(jq -nc --arg username "$LOCAL_ADMIN_USER" --arg password "$LOCAL_ADMIN_PASSWORD" '{username:$username,password:$password}')
access_token=$(curl -fsS -X POST "$API_URL/api/v1/auth/login" -H 'Content-Type: application/json' --data "$login_payload" | jq -er .data.accessToken)
auth_header="Authorization: Bearer $access_token"

register_release() {
  proof_name=$1
  labels=$2
  payload=$(jq -nc --arg version "proof-$proof_name" --arg deployedAt "$(date -u +%FT%TZ)" --argjson labels "$labels" \
    '{service:"sentinel-demo-api",environment:"development",version:$version,deployedAt:$deployedAt,labels:$labels}')
  curl -fsS -X POST "$API_URL/api/v1/releases" -H "$auth_header" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: telemetry-$proof_name-$(date +%s)-$$" --data "$payload" | jq -er .data.id
}

start_validation() {
  release_id=$1
  curl -fsS -X POST "$API_URL/api/v1/releases/$release_id/validate" -H "$auth_header" \
    -H 'Content-Type: application/json' --data '{"mode":"standard"}' | jq -er .data.id
}

wait_validation() {
  validation_id=$1
  destination=$2
  attempt=0
  while [ "$attempt" -lt 60 ]; do
    response=$(curl -fsS "$API_URL/api/v1/validations/$validation_id" -H "$auth_header")
    if [ "$(printf '%s' "$response" | jq -r .data.status)" = COMPLETED ]; then
      printf '%s\n' "$response" | jq .data > "$destination"
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 2
  done
  echo "Timeout aguardando validação $validation_id" >&2
  return 1
}

pass_labels=$(jq -nc '{
  health_url:"http://demo-api:8090/health",
  gate_promql:"vector(0)",gate_promql_max:"0",gate_promql_min_samples:"1",
  gate_logql:"sum(count_over_time({service_name=\"sentinel-demo-api\"}[15m]))",gate_logql_max:"1000000",gate_logql_min_samples:"1",
  gate_slo_promql:"vector(0)",gate_slo_promql_max:"1",gate_slo_promql_min_samples:"1",
  gate_traceql:"{ resource.service.name = \"sentinelops-proof-none\" }",gate_traceql_max_matches:"0"
}')
inconclusive_labels='{"health_url":"http://demo-api:8090/health"}'
fail_labels=$(printf '%s' "$pass_labels" | jq '.gate_promql="vector(2)" | .gate_promql_max="1"')

for expected in pass inconclusive fail; do
  case "$expected" in
    pass) labels=$pass_labels ;;
    inconclusive) labels=$inconclusive_labels ;;
    fail) labels=$fail_labels ;;
  esac
  release_id=$(register_release "$expected" "$labels")
  validation_id=$(start_validation "$release_id")
  wait_validation "$validation_id" "$ROOT/artifacts/validation/telemetry-$expected.json"
done

pass_result=$(jq -r .result "$ROOT/artifacts/validation/telemetry-pass.json")
pass_checks=$(jq '[.checks[] | select(.status=="PASS")] | length' "$ROOT/artifacts/validation/telemetry-pass.json")
inconclusive_result=$(jq -r .result "$ROOT/artifacts/validation/telemetry-inconclusive.json")
fail_result=$(jq -r .result "$ROOT/artifacts/validation/telemetry-fail.json")
printf 'standard_pass_result=%s\nstandard_pass_checks=%s\nmissing_policy_result=%s\nover_threshold_result=%s\n' \
  "$pass_result" "$pass_checks" "$inconclusive_result" "$fail_result"
[ "$pass_result" = PASS ] && [ "$pass_checks" -eq 5 ] && \
  [ "$inconclusive_result" = INCONCLUSIVE ] && [ "$fail_result" = FAIL ]
