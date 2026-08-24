#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
PROMETHEUS_URL=$(printenv PROMETHEUS_URL 2>/dev/null || printf http://127.0.0.1:9090)
GRAFANA_URL=$(printenv GRAFANA_URL 2>/dev/null || printf http://127.0.0.1:3001)
ARTIFACT_DIR="$ROOT/artifacts/dashboards"

die() { printf '%s\n' "[sentinelops][erro] $*" >&2; exit 1; }
for command_name in curl jq; do command -v "$command_name" >/dev/null 2>&1 || die "$command_name é obrigatório"; done

"$ROOT/scripts/generate-dashboards.sh" >/dev/null
for dashboard in "$ROOT"/dashboards/managed/*.json; do jq -e . "$dashboard" >/dev/null || die "JSON inválido: $dashboard"; done

linux="$ROOT/dashboards/managed/linux-overview.json"
application="$ROOT/dashboards/managed/application-overview.json"
docker="$ROOT/dashboards/managed/docker-overview.json"
jq -e '[.. | objects | .expr? | select(type == "string")] | join(" ") | contains("node_cpu_seconds_total") and contains("node_memory_MemAvailable_bytes") and contains("node_filesystem_avail_bytes") and contains("linux-system")' "$linux" >/dev/null || die "Dashboard Linux não cobre CPU, memória, filesystem e logs."
jq -e '[.. | objects | (.expr?, .query?, .labelSelector?) | select(type == "string")] | join(" ") | contains("demo_pipeline_requests_total") and contains("http_server_request_duration_seconds") and contains("sentinelops_apm_bootstrap") and contains("service_name") and contains("resource.service.name")' "$application" >/dev/null || die "Dashboard APM não cobre RED dos mocks e OpenTelemetry, logs, traces e profiles."
jq -e '[.. | objects | .expr? | select(type == "string")] | join(" ") | contains("container_cpu_usage_seconds_total") and contains("container_memory_working_set_bytes")' "$docker" >/dev/null || die "Dashboard Docker sem CPU e memória."

prom_query() {
  query=$1
  attempts=0
  while [ "$attempts" -lt 30 ]; do
    result=$(curl -fsS --get --max-time 10 --data-urlencode "query=$query" "$PROMETHEUS_URL/api/v1/query" 2>/dev/null || true)
    if printf '%s' "$result" | jq -e '.status == "success" and (.data.result | length) > 0' >/dev/null 2>&1; then
      printf '%s\n' "$result"
      return 0
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  die "Prometheus não retornou dados para: $query"
}

host_up=$(prom_query 'up{job="linux-node",instance="sentinel-demo-linux"}')
host_inventory=$(prom_query 'node_uname_info{instance="sentinel-demo-linux"}')
app_requests=$(prom_query 'demo_pipeline_requests_total{service="sentinel-demo-api"}')
curl -fsS --max-time 10 "$GRAFANA_URL/api/health" | jq -e '.database == "ok"' >/dev/null || die "Grafana não está saudável."

admin_user=$(awk -F= '$1=="GRAFANA_ADMIN_USER"{print substr($0,index($0,"=")+1)}' "$ROOT/.env")
admin_password=$(awk -F= '$1=="GRAFANA_ADMIN_PASSWORD"{print substr($0,index($0,"=")+1)}' "$ROOT/.env")
for uid in sentinel-linux-overview sentinel-application-overview sentinel-apm sentinel-docker-overview; do
  attempts=0
  until curl -fsS --max-time 10 -u "$admin_user:$admin_password" "$GRAFANA_URL/api/dashboards/uid/$uid" | jq -e --arg uid "$uid" '.dashboard.uid == $uid' >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 15 ] || die "Dashboard não provisionado no Grafana: $uid"
    sleep 2
  done
done

mkdir -p "$ARTIFACT_DIR"
artifact="$ARTIFACT_DIR/proof-$(date -u +%Y%m%dT%H%M%SZ).json"
jq -n --arg provedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson dashboards "$(find "$ROOT/dashboards/managed" -name '*.json' | wc -l | tr -d ' ')" \
  --argjson hostSeries "$(printf '%s' "$host_up" | jq '.data.result | length')" \
  --argjson inventorySeries "$(printf '%s' "$host_inventory" | jq '.data.result | length')" \
  --argjson appSeries "$(printf '%s' "$app_requests" | jq '.data.result | length')" \
  '{provedAt:$provedAt,dashboards:$dashboards,provisioned:["sentinel-linux-overview","sentinel-application-overview","sentinel-apm","sentinel-docker-overview"],liveData:{linuxUpSeries:$hostSeries,linuxInventorySeries:$inventorySeries,applicationRequestSeries:$appSeries}}' > "$artifact"
chmod 600 "$artifact"
printf 'Dashboards especializados e dados vivos comprovados: %s\n' "$artifact"
