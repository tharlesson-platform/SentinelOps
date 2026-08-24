#!/bin/sh
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
# shellcheck disable=SC1091
. "$ROOT/.env"
API_URL=${SENTINEL_API_URL:-http://localhost:8080}
TOKEN=$(curl -fsS -X POST "$API_URL/api/v1/auth/login" -H 'Content-Type: application/json' --data "{\"username\":\"$LOCAL_ADMIN_USER\",\"password\":\"$LOCAL_ADMIN_PASSWORD\"}" | jq -er '.data.accessToken')
AUTH="Authorization: Bearer $TOKEN"
for service_file in "$ROOT"/examples/services/sentinel-demo-*.json; do
  curl -fsS -X POST "$API_URL/api/v1/services" -H "$AUTH" -H 'Content-Type: application/json' --data-binary @"$service_file" >/dev/null
done
for scenario_file in "$ROOT"/examples/scenarios/demo-*.json; do
  curl -fsS -X POST "$API_URL/api/v1/scenarios" -H "$AUTH" -H 'Content-Type: application/json' --data-binary @"$scenario_file" >/dev/null
done
echo "Catálogo dos três mocks e cenários demonstrativos carregados."
