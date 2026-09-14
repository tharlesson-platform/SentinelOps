#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
patterns='sentinel-demo|demo_pipeline|local-demo|demo-only|demo-api|demo-orders|demo-payments|Dockerfile\.demo|DEMO_BASE_URL'

entrypoint_files="
$ROOT/Makefile
$ROOT/scripts/generate-dashboards.sh
$ROOT/scripts/render-production-dashboards.sh
$ROOT/scripts/doctor.sh
$ROOT/scripts/lock-local-images.sh
$ROOT/scripts/sign-local-image-lock.sh
$ROOT/scripts/upgrade-local.sh
$ROOT/scripts/install-linux-server.sh
"

runtime_matches=$(find \
  "$ROOT/deploy" \
  "$ROOT/dashboards/managed" \
  "$ROOT/.github" \
  "$ROOT/artifacts/runtime" \
  "$ROOT/apps/api" \
  "$ROOT/apps/worker" \
  "$ROOT/internal" \
  -type f ! -name '*_test.go' -exec grep -nEiH "$patterns" {} + 2>/dev/null || true)
# shellcheck disable=SC2086
entrypoint_matches=$(grep -nEiH "$patterns" $entrypoint_files 2>/dev/null || true)
if [ -n "$runtime_matches$entrypoint_matches" ]; then
  printf '%s\n%s\n' "$runtime_matches" "$entrypoint_matches" | sed '/^$/d'
  echo "Falha: caminho produtivo ainda referencia telemetria não produtiva." >&2
  exit 1
fi

echo "Caminhos produtivos livres de telemetria não produtiva."
