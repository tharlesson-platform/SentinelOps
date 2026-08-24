#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
OUT="$ROOT/dashboards/managed"
mkdir -p "$OUT"

write_dashboard() {
  uid=$1
  title=$2
  tags=$3
  panels=$4
  variables=$5
  cat <<EOF
{
  "annotations": {"list": [{"builtIn": 1, "datasource": {"type": "grafana", "uid": "-- Grafana --"}, "enable": true, "hide": true, "name": "Deployments", "type": "dashboard"}]},
  "editable": false,
  "graphTooltip": 1,
  "links": [{"title": "SentinelOps", "type": "link", "url": "http://localhost:3000"}],
  "panels": $panels,
  "refresh": "15s",
  "schemaVersion": 42,
  "tags": ["sentinelops", "managed", "$tags"],
  "templating": {"list": $variables},
  "time": {"from": "now-30m", "to": "now"},
  "timezone": "browser",
  "title": "$title",
  "uid": "sentinel-$uid",
  "version": 2
}
EOF
}

generic_dashboard() {
  uid=$1
  title=$2
  panels='[
    {"type":"stat","title":"Disponibilidade","id":1,"gridPos":{"h":5,"w":6,"x":0,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"min(up{job=\"sentinel-demo-apps\"})","refId":"A"}]},
    {"type":"timeseries","title":"Requests por segundo","id":2,"gridPos":{"h":8,"w":9,"x":6,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum by (service, route) (rate(demo_pipeline_requests_total[5m]))","legendFormat":"{{service}} · {{route}}","refId":"A"}]},
    {"type":"timeseries","title":"Latência p95","id":3,"gridPos":{"h":8,"w":9,"x":15,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"histogram_quantile(0.95, sum by (le, service, route) (rate(demo_pipeline_request_duration_seconds_bucket[5m])))","legendFormat":"{{service}} · {{route}}","refId":"A"}]},
    {"type":"logs","title":"Logs correlacionados","id":4,"gridPos":{"h":9,"w":12,"x":0,"y":8},"datasource":{"type":"loki","uid":"loki"},"targets":[{"expr":"{service_namespace=\"sentinel-demo\"}","refId":"A"}]},
    {"type":"traces","title":"Traces distribuídos","id":5,"gridPos":{"h":9,"w":12,"x":12,"y":8},"datasource":{"type":"tempo","uid":"tempo"},"targets":[{"query":"{ resource.service.namespace = \"sentinel-demo\" }","queryType":"traceql","refId":"A"}]}
  ]'
  variables='[{"name":"environment","label":"Ambiente","type":"custom","query":"local-demo,development,staging,production","current":{"text":"local-demo","value":"local-demo"}}]'
  write_dashboard "$uid" "$title" "$uid" "$panels" "$variables"
}

linux_dashboard() {
  panels='[
    {"type":"stat","title":"Hosts ativos","id":1,"gridPos":{"h":4,"w":4,"x":0,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum(up{job=\"linux-node\",instance=~\"$instance\",deployment_environment=~\"$environment\"})","refId":"A"}]},
    {"type":"stat","title":"Uptime","id":2,"gridPos":{"h":4,"w":4,"x":4,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"time() - max(node_boot_time_seconds{instance=~\"$instance\",deployment_environment=~\"$environment\"})","refId":"A"}],"fieldConfig":{"defaults":{"unit":"s"},"overrides":[]}},
    {"type":"stat","title":"CPU utilizada","id":3,"gridPos":{"h":4,"w":4,"x":8,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"100 - avg(rate(node_cpu_seconds_total{instance=~\"$instance\",deployment_environment=~\"$environment\",mode=\"idle\"}[5m])) * 100","refId":"A"}],"fieldConfig":{"defaults":{"unit":"percent","min":0,"max":100},"overrides":[]}},
    {"type":"stat","title":"Memória utilizada","id":4,"gridPos":{"h":4,"w":4,"x":12,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"(1 - node_memory_MemAvailable_bytes{instance=~\"$instance\",deployment_environment=~\"$environment\"} / node_memory_MemTotal_bytes{instance=~\"$instance\",deployment_environment=~\"$environment\"}) * 100","refId":"A"}],"fieldConfig":{"defaults":{"unit":"percent","min":0,"max":100},"overrides":[]}},
    {"type":"stat","title":"Filesystem utilizado","id":5,"gridPos":{"h":4,"w":4,"x":16,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"max((1 - node_filesystem_avail_bytes{instance=~\"$instance\",deployment_environment=~\"$environment\",fstype!~\"tmpfs|overlay\"} / node_filesystem_size_bytes{instance=~\"$instance\",deployment_environment=~\"$environment\",fstype!~\"tmpfs|overlay\"}) * 100)","refId":"A"}],"fieldConfig":{"defaults":{"unit":"percent","min":0,"max":100},"overrides":[]}},
    {"type":"stat","title":"Processos","id":6,"gridPos":{"h":4,"w":4,"x":20,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum(node_processes_running{instance=~\"$instance\",deployment_environment=~\"$environment\"})","refId":"A"}]},
    {"type":"timeseries","title":"CPU por host","id":7,"gridPos":{"h":8,"w":8,"x":0,"y":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"100 - avg by (instance) (rate(node_cpu_seconds_total{instance=~\"$instance\",deployment_environment=~\"$environment\",mode=\"idle\"}[5m])) * 100","legendFormat":"{{instance}}","refId":"A"}]},
    {"type":"timeseries","title":"Memória por host","id":8,"gridPos":{"h":8,"w":8,"x":8,"y":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"(1 - node_memory_MemAvailable_bytes{instance=~\"$instance\",deployment_environment=~\"$environment\"} / node_memory_MemTotal_bytes{instance=~\"$instance\",deployment_environment=~\"$environment\"}) * 100","legendFormat":"{{instance}}","refId":"A"}]},
    {"type":"timeseries","title":"Rede RX/TX","id":9,"gridPos":{"h":8,"w":8,"x":16,"y":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum by (instance) (rate(node_network_receive_bytes_total{instance=~\"$instance\",deployment_environment=~\"$environment\",device!=\"lo\"}[5m]))","legendFormat":"{{instance}} RX","refId":"A"},{"expr":"sum by (instance) (rate(node_network_transmit_bytes_total{instance=~\"$instance\",deployment_environment=~\"$environment\",device!=\"lo\"}[5m]))","legendFormat":"{{instance}} TX","refId":"B"}]},
    {"type":"timeseries","title":"Filesystem por montagem","id":10,"gridPos":{"h":8,"w":12,"x":0,"y":12},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"(1 - node_filesystem_avail_bytes{instance=~\"$instance\",deployment_environment=~\"$environment\",fstype!~\"tmpfs|overlay\"} / node_filesystem_size_bytes{instance=~\"$instance\",deployment_environment=~\"$environment\",fstype!~\"tmpfs|overlay\"}) * 100","legendFormat":"{{instance}} {{mountpoint}}","refId":"A"}]},
    {"type":"table","title":"Inventário Linux","id":11,"gridPos":{"h":8,"w":12,"x":12,"y":12},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"node_uname_info{instance=~\"$instance\",deployment_environment=~\"$environment\"}","format":"table","instant":true,"refId":"A"}]},
    {"type":"logs","title":"Logs do sistema","id":12,"gridPos":{"h":9,"w":24,"x":0,"y":20},"datasource":{"type":"loki","uid":"loki"},"targets":[{"expr":"{job=\"linux-system\",host_name=~\"$instance\",deployment_environment=~\"$environment\"}","refId":"A"}]}
  ]'
  variables='[
    {"name":"environment","label":"Ambiente","type":"query","datasource":{"type":"prometheus","uid":"prometheus"},"query":{"query":"label_values(up{job=\"linux-node\"}, deployment_environment)","refId":"env"},"includeAll":true,"allValue":".*","refresh":1},
    {"name":"instance","label":"Host","type":"query","datasource":{"type":"prometheus","uid":"prometheus"},"query":{"query":"label_values(up{job=\"linux-node\",deployment_environment=~\"$environment\"}, instance)","refId":"host"},"includeAll":true,"allValue":".*","refresh":1}
  ]'
  write_dashboard linux-overview "Linux Hosts - Fleet e Detalhe" linux "$panels" "$variables"
}

application_dashboard() {
  uid=$1
  title=$2
  panels='[
    {"type":"stat","title":"Telemetria ativa","id":1,"gridPos":{"h":4,"w":4,"x":0,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum(up{job=\"sentinel-demo-apps\",service_name=~\"$service\"}) or clamp_max(count(sentinelops_apm_bootstrap{job=~\"$service\"} or http_server_request_duration_seconds_count{job=~\"$service\"}), 1)","refId":"A"}]},
    {"type":"stat","title":"Requests/s","id":2,"gridPos":{"h":4,"w":4,"x":4,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum(rate(demo_pipeline_requests_total{service=~\"$service\"}[5m])) or sum(rate(http_server_request_duration_seconds_count{job=~\"$service\"}[5m]))","refId":"A"}]},
    {"type":"stat","title":"Taxa de erro","id":3,"gridPos":{"h":4,"w":4,"x":8,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum(rate(demo_pipeline_requests_total{service=~\"$service\",status=~\"5..\"}[5m])) / clamp_min(sum(rate(demo_pipeline_requests_total{service=~\"$service\"}[5m])), 0.000001) * 100 or sum(rate(http_server_request_duration_seconds_count{job=~\"$service\",http_response_status_code=~\"5..\"}[5m])) / clamp_min(sum(rate(http_server_request_duration_seconds_count{job=~\"$service\"}[5m])), 0.000001) * 100","refId":"A"}]},
    {"type":"stat","title":"Latência p95","id":4,"gridPos":{"h":4,"w":4,"x":12,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"histogram_quantile(0.95, sum by (le) (rate(demo_pipeline_request_duration_seconds_bucket{service=~\"$service\"}[5m]))) or histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket{job=~\"$service\"}[5m])))","refId":"A"}]},
    {"type":"stat","title":"Erros downstream/s","id":5,"gridPos":{"h":4,"w":4,"x":16,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum(rate(demo_pipeline_downstream_errors_total{service=~\"$service\"}[5m]))","refId":"A"}]},
    {"type":"stat","title":"SLO disponibilidade","id":6,"gridPos":{"h":4,"w":4,"x":20,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"1 - sum(rate(demo_pipeline_requests_total{service=~\"$service\",status=~\"5..\"}[30m])) / clamp_min(sum(rate(demo_pipeline_requests_total{service=~\"$service\"}[30m])), 0.000001) or 1 - sum(rate(http_server_request_duration_seconds_count{job=~\"$service\",http_response_status_code=~\"5..\"}[30m])) / clamp_min(sum(rate(http_server_request_duration_seconds_count{job=~\"$service\"}[30m])), 0.000001)","refId":"A"}]},
    {"type":"timeseries","title":"RED - volume por serviço e rota","id":7,"gridPos":{"h":8,"w":12,"x":0,"y":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum by (service,route) (rate(demo_pipeline_requests_total{service=~\"$service\"}[5m]))","legendFormat":"{{service}} · {{route}}","refId":"A"},{"expr":"sum by (job,http_route) (rate(http_server_request_duration_seconds_count{job=~\"$service\"}[5m]))","legendFormat":"{{job}} · {{http_route}}","refId":"B"}]},
    {"type":"timeseries","title":"RED - latência p50/p95/p99","id":8,"gridPos":{"h":8,"w":12,"x":12,"y":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"histogram_quantile(0.50, sum by (le,service) (rate(demo_pipeline_request_duration_seconds_bucket{service=~\"$service\"}[5m])))","legendFormat":"{{service}} p50","refId":"A"},{"expr":"histogram_quantile(0.95, sum by (le,service) (rate(demo_pipeline_request_duration_seconds_bucket{service=~\"$service\"}[5m])))","legendFormat":"{{service}} p95","refId":"B"},{"expr":"histogram_quantile(0.99, sum by (le,service) (rate(demo_pipeline_request_duration_seconds_bucket{service=~\"$service\"}[5m])))","legendFormat":"{{service}} p99","refId":"C"},{"expr":"histogram_quantile(0.95, sum by (le,job) (rate(http_server_request_duration_seconds_bucket{job=~\"$service\"}[5m])))","legendFormat":"{{job}} p95 OTel","refId":"D"}]},
    {"type":"logs","title":"Logs da aplicação","id":9,"gridPos":{"h":9,"w":12,"x":0,"y":12},"datasource":{"type":"loki","uid":"loki"},"targets":[{"expr":"{service_name=~\"$service\"}","refId":"A"}]},
    {"type":"traces","title":"Traces distribuídos","id":10,"gridPos":{"h":9,"w":12,"x":12,"y":12},"datasource":{"type":"tempo","uid":"tempo"},"targets":[{"query":"{ resource.service.name =~ \"$service\" }","queryType":"traceql","refId":"A"}]},
    {"type":"timeseries","title":"CPU profiles","id":11,"gridPos":{"h":9,"w":24,"x":0,"y":21},"datasource":{"type":"grafana-pyroscope-datasource","uid":"pyroscope"},"targets":[{"queryType":"profile","profileTypeId":"process_cpu:cpu:nanoseconds:cpu:nanoseconds","labelSelector":"{service_name=~\"$service\"}","refId":"A"}]}
  ]'
  variables='[{"name":"service","label":"Aplicação","type":"query","datasource":{"type":"prometheus","uid":"prometheus"},"query":{"query":"query_result(count by (service) (demo_pipeline_requests_total) or label_replace(count by (job) (sentinelops_apm_bootstrap{job!=\"alloy\"} or http_server_request_duration_seconds_count{job!=\"alloy\"}), \"service\", \"$1\", \"job\", \"(.*)\"))","refId":"service"},"regex":"/service=\"([^\"]+)\"/","includeAll":true,"allValue":".*","refresh":1}]'
  write_dashboard "$uid" "$title" application "$panels" "$variables"
}

docker_dashboard() {
  panels='[
    {"type":"stat","title":"Containers observados","id":1,"gridPos":{"h":4,"w":6,"x":0,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"count(container_last_seen{name!=\"\"})","refId":"A"}]},
    {"type":"timeseries","title":"CPU por container","id":2,"gridPos":{"h":8,"w":9,"x":6,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum by (instance,name) (rate(container_cpu_usage_seconds_total{name=~\"$container\"}[5m])) * 100","legendFormat":"{{instance}} · {{name}}","refId":"A"}]},
    {"type":"timeseries","title":"Memória por container","id":3,"gridPos":{"h":8,"w":9,"x":15,"y":0},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"container_memory_working_set_bytes{name=~\"$container\"}","legendFormat":"{{instance}} · {{name}}","refId":"A"}]},
    {"type":"timeseries","title":"Rede por container","id":4,"gridPos":{"h":8,"w":12,"x":0,"y":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum by (instance,name) (rate(container_network_receive_bytes_total{name=~\"$container\"}[5m]))","legendFormat":"{{name}} RX","refId":"A"},{"expr":"sum by (instance,name) (rate(container_network_transmit_bytes_total{name=~\"$container\"}[5m]))","legendFormat":"{{name}} TX","refId":"B"}]},
    {"type":"timeseries","title":"I/O de filesystem","id":5,"gridPos":{"h":8,"w":12,"x":12,"y":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"expr":"sum by (instance,name) (rate(container_fs_reads_bytes_total{name=~\"$container\"}[5m]) + rate(container_fs_writes_bytes_total{name=~\"$container\"}[5m]))","legendFormat":"{{instance}} · {{name}}","refId":"A"}]}
  ]'
  variables='[{"name":"container","label":"Container","type":"query","datasource":{"type":"prometheus","uid":"prometheus"},"query":{"query":"label_values(container_last_seen{name!=\"\"}, name)","refId":"container"},"includeAll":true,"allValue":".*","refresh":1}]'
  write_dashboard docker-overview "Docker Containers" docker "$panels" "$variables"
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
  case "$uid" in
    linux-overview) linux_dashboard > "$OUT/$uid.json" ;;
    application-overview|apm) application_dashboard "$uid" "$safe_title" > "$OUT/$uid.json" ;;
    docker-overview) docker_dashboard > "$OUT/$uid.json" ;;
    *) generic_dashboard "$uid" "$safe_title" > "$OUT/$uid.json" ;;
  esac
done

echo "Dashboards gerados: hosts Linux, aplicações/APM, containers e catálogo gerenciado."
