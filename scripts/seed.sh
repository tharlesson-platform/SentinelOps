#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/.env"
API_URL=${SENTINEL_API_URL:-http://localhost:8080}
TOKEN=$(curl -fsS -X POST "$API_URL/api/v1/auth/login" -H 'Content-Type: application/json' --data "{\"username\":\"$LOCAL_ADMIN_USER\",\"password\":\"$LOCAL_ADMIN_PASSWORD\"}" | jq -er '.data.accessToken')
AUTH="Authorization: Bearer $TOKEN"
curl -fsS -X POST "$API_URL/api/v1/services" -H "$AUTH" -H 'Content-Type: application/json' --data-binary @"$ROOT/examples/services/sentinel-demo-api.json" >/dev/null
curl -fsS -X POST "$API_URL/api/v1/scenarios" -H "$AUTH" -H 'Content-Type: application/json' --data-binary @"$ROOT/examples/scenarios/demo-health.json" >/dev/null
echo "Catálogo e cenário demonstrativo carregados."

