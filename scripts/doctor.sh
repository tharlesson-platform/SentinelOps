#!/bin/sh
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
COMPOSE="docker compose --env-file $ROOT/.env -f $ROOT/deploy/compose/docker-compose.yml"
$COMPOSE config --quiet
wait_ready() {
  url=$1
  limit=${2:-30}
  attempts=0
  until curl --max-time 4 -fsS "$url" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt "$limit" ] || { echo "Timeout aguardando $url" >&2; return 1; }
    sleep 3
  done
}
wait_ready http://localhost:8080/readyz
wait_ready http://localhost:3000/healthz
wait_ready http://localhost:8090/health
wait_ready http://localhost:12345/-/ready
wait_ready http://localhost:9090/-/ready
wait_ready http://localhost:3100/ready
wait_ready http://localhost:3200/ready
wait_ready http://localhost:4040/ready 90
chain=$(curl -fsS -H 'X-Demo-Marker: doctor' -H 'X-Synthetic-Test: sentinelops' http://localhost:8090/api/checkout)
printf '%s' "$chain" | jq -e '
  .service == "sentinel-demo-api" and
  .downstream.service == "sentinel-demo-orders" and
  .downstream.downstream.service == "sentinel-demo-payments" and
  .traceId == .downstream.traceId and .traceId == .downstream.downstream.traceId
' >/dev/null
echo "Control Plane, Web, Alloy, cadeia mock e backends de telemetria responderam."
