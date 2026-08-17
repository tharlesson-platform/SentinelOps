#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT="$ROOT/dashboards/managed"
mkdir -p "$OUT"
apply_dashboard() {
  uid=$1
  title=$2
  cat <<EOF
{
  "annotations": {"list": [{"builtIn": 1, "datasource": {"type": "grafana", "uid": "-- Grafana --"}, "enable": true, "hide": true, "iconColor": "rgba(67, 230, 189, 1)", "name": "Deployments", "type": "dashboard"}]},
  "editable": false,
  "graphTooltip": 1,
  "links": [{"title": "SentinelOps", "type": "link", "url": "http://localhost:3000"}],
  "panels": [
    {"type": "stat", "title": "Disponibilidade do SentinelOps", "id": 1, "gridPos": {"h": 5, "w": 6, "x": 0, "y": 0}, "datasource": {"type": "prometheus", "uid": "prometheus"}, "targets": [{"expr": "max(up{job=~\"sentinel-api|sentinel-demo-api\"})", "refId": "A"}], "fieldConfig": {"defaults": {"unit": "percentunit", "min": 0, "max": 1, "thresholds": {"mode": "absolute", "steps": [{"color": "red", "value": null}, {"color": "green", "value": 1}]}}, "overrides": []}},
    {"type": "timeseries", "title": "Requests por segundo", "id": 2, "gridPos": {"h": 8, "w": 9, "x": 6, "y": 0}, "datasource": {"type": "prometheus", "uid": "prometheus"}, "targets": [{"expr": "sum by (route) (rate(demo_http_requests_total[5m]))", "legendFormat": "{{route}}", "refId": "A"}], "fieldConfig": {"defaults": {"unit": "reqps"}, "overrides": []}},
    {"type": "timeseries", "title": "Latência p95", "id": 3, "gridPos": {"h": 8, "w": 9, "x": 15, "y": 0}, "datasource": {"type": "prometheus", "uid": "prometheus"}, "targets": [{"expr": "histogram_quantile(0.95, sum by (le, route) (rate(demo_http_request_duration_seconds_bucket[5m])))", "legendFormat": "{{route}}", "refId": "A"}], "fieldConfig": {"defaults": {"unit": "s"}, "overrides": []}},
    {"type": "logs", "title": "Logs correlacionados", "id": 4, "gridPos": {"h": 9, "w": 12, "x": 0, "y": 8}, "datasource": {"type": "loki", "uid": "loki"}, "targets": [{"expr": "{service_name=~\".+\"}", "refId": "A"}]},
    {"type": "traces", "title": "Traces recentes", "id": 5, "gridPos": {"h": 9, "w": 12, "x": 12, "y": 8}, "datasource": {"type": "tempo", "uid": "tempo"}, "targets": [{"query": "{ resource.service.name = \"sentinel-demo-api\" }", "queryType": "traceql", "refId": "A"}]}
  ],
  "refresh": "15s",
  "schemaVersion": 42,
  "tags": ["sentinelops", "managed", "$uid"],
  "templating": {"list": [{"name": "environment", "type": "custom", "query": "local-demo,development,staging,production", "current": {"text": "local-demo", "value": "local-demo"}}]},
  "time": {"from": "now-30m", "to": "now"},
  "timezone": "browser",
  "title": "$title",
  "uid": "sentinel-$uid",
  "version": 1
}
EOF
}
dashboards='executive-overview|Executive Overview
noc-overview|NOC Overview
service-health|Service Health
application-overview|Application Overview
apm|APM
throughput|Throughput
errors|Errors
latency|Latency
distributed-tracing|Distributed Tracing
service-graph|Service Graph
logs|Logs
continuous-profiling|Continuous Profiling
frontend-observability|Frontend Observability
web-vitals|Web Vitals
synthetic-monitoring|Synthetic Monitoring
browser-test-results|Browser Test Results
api-test-results|API Test Results
release-validation|Release Validation
release-comparison|Release Comparison
slo-error-budget|SLO and Error Budget
kubernetes-overview|Kubernetes Overview
ecs-overview|ECS Overview
linux-overview|Linux Overview
docker-overview|Docker Overview
aws-overview|AWS Overview
azure-overview|Azure Overview
vmware-overview|VMware Overview
database-overview|Database Overview
messaging-overview|Messaging Overview
capacity-forecast|Capacity and Forecast
alerts|Alerts
incident-timeline|Incident Timeline
agent-fleet|Agent Fleet
cardinality-ingestion-cost|Cardinality and Ingestion Cost
sentinelops-self-monitoring|SentinelOps Self-Monitoring'
printf '%s\n' "$dashboards" | while IFS='|' read -r uid title; do
  safe_title=$(printf '%s' "$title" | sed 's/"/\\"/g')
  apply_dashboard "$uid" "$safe_title" > "$OUT/$uid.json"
done
