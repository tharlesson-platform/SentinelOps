#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/.env"
API=${SENTINEL_API_URL:-http://localhost:8080}
TOKEN=$(curl -fsS -X POST "$API/api/v1/auth/login" -H 'Content-Type: application/json' --data "{\"username\":\"$LOCAL_ADMIN_USER\",\"password\":\"$LOCAL_ADMIN_PASSWORD\"}" | jq -er .data.accessToken)
AUTH="Authorization: Bearer $TOKEN"
register() { name=$1; url=$2; curl -fsS -X POST "$API/api/v1/releases" -H "$AUTH" -H 'Content-Type: application/json' -H "Idempotency-Key: proof-$name-$(date +%s)" --data "{\"service\":\"sentinel-demo-api\",\"environment\":\"development\",\"version\":\"$name\",\"deployedAt\":\"$(date -u +%FT%TZ)\",\"labels\":{\"health_url\":\"$url\"}}" | jq -er .data.id; }
validate() { release=$1; curl -fsS -X POST "$API/api/v1/releases/$release/validate" -H "$AUTH" -H 'Content-Type: application/json' --data '{"mode":"smoke"}' | jq -er .data.id; }
wait_result() { validation=$1; i=0; while [ "$i" -lt 60 ]; do data=$(curl -fsS "$API/api/v1/validations/$validation" -H "$AUTH"); [ "$(printf '%s' "$data" | jq -r .data.status)" = COMPLETED ] && { printf '%s' "$data" | jq .data; return; }; i=$((i+1)); sleep 2; done; return 1; }
healthy=$(register healthy http://demo-api:8090/health); healthy_validation=$(validate "$healthy"); wait_result "$healthy_validation" | tee "$ROOT/artifacts/healthy-release.json" | jq -e '.result=="PASS"' >/dev/null
bad=$(register bad http://demo-api:8090/health?fault=error); bad_validation=$(validate "$bad"); wait_result "$bad_validation" | tee "$ROOT/artifacts/bad-release.json" | jq -e '.result=="FAIL"' >/dev/null
echo "Evidências PASS e FAIL gravadas em artifacts/."

