#!/bin/sh
# Compatibility entrypoint retained for operators and automation. Managed
# dashboards are production-source artifacts and may never be regenerated from
# synthetic application data.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

"$ROOT/scripts/render-production-dashboards.sh"
python3 "$ROOT/scripts/check-dashboard-filters.py"
"$ROOT/scripts/check-no-nonprod-telemetry.sh"

echo "Dashboards reconciliadas exclusivamente com fontes reais de produção."
