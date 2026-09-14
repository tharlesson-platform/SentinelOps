#!/usr/bin/env bash
set -euo pipefail

# Replaces the former demo-shaped managed dashboards with real-source queries.
# A dashboard for an integration that is not onboarded intentionally reports the
# collector as absent; it must never borrow another domain's telemetry.
root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
dashboard_dir="$root_dir/dashboards/managed"

for dashboard in "$dashboard_dir"/*.json; do
  name=$(basename "$dashboard")
  case "$name" in
    apm.json|application-overview.json|distributed-tracing.json|errors.json|latency.json|service-graph.json|service-health.json|slo-error-budget.json|throughput.json)
      profile=application
      ;;
    agent-fleet.json|azure-overview.json|capacity-forecast.json|executive-overview.json|noc-overview.json)
      profile=host
      ;;
    api-test-results.json|browser-test-results.json|incident-timeline.json|release-comparison.json|release-validation.json|synthetic-monitoring.json)
      profile=synthetic
      ;;
    alerts.json|cardinality-ingestion-cost.json|logs.json|sentinelops-self-monitoring.json)
      profile=control
      ;;
    frontend-observability.json|web-vitals.json)
      profile=frontend
      ;;
    database-overview.json)
      profile=database
      ;;
    aws-overview.json|continuous-profiling.json|ecs-overview.json|kubernetes-overview.json|messaging-overview.json|vmware-overview.json)
      profile=absent-source
      ;;
    *)
      continue
      ;;
  esac

  tmp=$(mktemp)
  case "$profile" in
    application)
      jq '
        .description = "Telemetria real das aplicações Java instrumentadas no tqi-platform."
        | .templating.list |= map(if .name == "environment" then .query = "production" | .current = {text:"production", value:"production"} else . end)
        | .panels[0].title = "Serviços APM com métricas" | .panels[0].targets[0].expr = "count(http_server_request_duration_seconds_count{job=~\"tqi-platform/.+\"})"
        | .panels[1].title = "Requisições por segundo" | .panels[1].targets[0].expr = "sum by (job) (rate(http_server_request_duration_seconds_count{job=~\"tqi-platform/.+\"}[5m]))" | .panels[1].targets[0].legendFormat = "{{job}}"
        | .panels[2].title = "Latência p95" | .panels[2].targets[0].expr = "histogram_quantile(0.95, sum by (le, job) (rate(http_server_request_duration_seconds_bucket{job=~\"tqi-platform/.+\"}[5m])))" | .panels[2].targets[0].legendFormat = "{{job}}"
        | .panels[3].title = "Logs reais dos contêineres" | .panels[3].targets[0].expr = "{job=\"docker-container\",host_name=\"tqi-platform\"}"
        | .panels[4].title = "Traços distribuídos" | .panels[4].targets[0].query = "{ resource.service.namespace = \"tqi-platform\" }"
      ' "$dashboard" > "$tmp"
      ;;
    host)
      jq '
        .description = "Métricas reais da frota Linux recebidas pelos coletores SentinelOps."
        | .templating.list |= map(if .name == "environment" then .query = "production" | .current = {text:"production", value:"production"} else . end)
        | .panels[0].title = "Hosts Linux ativos" | .panels[0].targets[0].expr = "count(up{job=\"linux-node\"} == 1)"
        | .panels[1].title = "CPU por host" | .panels[1].targets[0].expr = "100 - avg by (instance) (rate(node_cpu_seconds_total{job=\"linux-node\",mode=\"idle\"}[5m])) * 100" | .panels[1].targets[0].legendFormat = "{{instance}}"
        | .panels[2].title = "Memória utilizada por host" | .panels[2].targets[0].expr = "100 * (1 - node_memory_MemAvailable_bytes{job=\"linux-node\"} / node_memory_MemTotal_bytes{job=\"linux-node\"})" | .panels[2].targets[0].legendFormat = "{{instance}}"
        | .panels[3].title = "Logs reais de aplicações" | .panels[3].targets[0].expr = "{job=\"docker-container\",host_name=\"tqi-platform\"}"
        | .panels[4].type = "stat" | .panels[4].title = "Contêineres observados" | .panels[4].datasource = {type:"prometheus",uid:"prometheus"} | .panels[4].targets = [{expr:"count(container_last_seen{name!=\"\"})",refId:"A"}]
      ' "$dashboard" > "$tmp"
      ;;
    synthetic)
      jq '
        .description = "Sondas HTTP reais executadas pela central SentinelOps."
        | .templating.list |= map(if .name == "environment" then .query = "production" | .current = {text:"production", value:"production"} else . end)
        | .panels[0].title = "Disponibilidade do alvo real" | .panels[0].targets[0].expr = "min(probe_success{job=\"blackbox-production\"})"
        | .panels[1].title = "Duração da sonda HTTP" | .panels[1].targets[0].expr = "probe_duration_seconds{job=\"blackbox-production\"}" | .panels[1].targets[0].legendFormat = "{{instance}}"
        | .panels[2].title = "Resolução DNS da sonda" | .panels[2].targets[0].expr = "probe_dns_lookup_time_seconds{job=\"blackbox-production\"}" | .panels[2].targets[0].legendFormat = "{{instance}}"
        | .panels[3].type = "stat" | .panels[3].title = "Sucesso da sonda" | .panels[3].datasource = {type:"prometheus",uid:"prometheus"} | .panels[3].targets = [{expr:"probe_success{job=\"blackbox-production\"}",refId:"A"}]
        | .panels[4].type = "stat" | .panels[4].title = "Código HTTP" | .panels[4].datasource = {type:"prometheus",uid:"prometheus"} | .panels[4].targets = [{expr:"probe_http_status_code{job=\"blackbox-production\"}",refId:"A"}]
      ' "$dashboard" > "$tmp"
      ;;
    control)
      jq '
        .description = "Estado real do plano de controle e da ingestão SentinelOps."
        | .templating.list |= map(if .name == "environment" then .query = "production" | .current = {text:"production", value:"production"} else . end)
        | .panels[0].title = "Alvos Prometheus saudáveis" | .panels[0].targets[0].expr = "count(up == 1)"
        | .panels[1].title = "Séries ativas no TSDB" | .panels[1].targets[0].expr = "prometheus_tsdb_head_series" | .panels[1].targets[0].legendFormat = "series"
        | .panels[2].title = "Amostras ingeridas por segundo" | .panels[2].targets[0].expr = "rate(prometheus_tsdb_head_samples_appended_total[5m])" | .panels[2].targets[0].legendFormat = "samples/s"
        | .panels[3].title = "Logs reais dos contêineres" | .panels[3].targets[0].expr = "{job=\"docker-container\",host_name=\"tqi-platform\"}"
        | .panels[4].type = "stat" | .panels[4].title = "Alertas em firing" | .panels[4].datasource = {type:"prometheus",uid:"prometheus"} | .panels[4].targets = [{expr:"count(ALERTS{alertstate=\"firing\"})",refId:"A"}]
      ' "$dashboard" > "$tmp"
      ;;
    frontend)
      jq '
        .description = "Runtime real dos contêineres frontend; RUM/Web Vitals requer instrumentação de browser em artefato futuro."
        | .templating.list |= map(if .name == "environment" then .query = "production" | .current = {text:"production", value:"production"} else . end)
        | .panels[0].title = "Frontends observados em Docker" | .panels[0].targets[0].expr = "count(container_last_seen{name=~\".*(frontend|web).*\"})"
        | .panels[1].title = "CPU dos frontends" | .panels[1].targets[0].expr = "sum by (name) (rate(container_cpu_usage_seconds_total{name=~\".*(frontend|web).*\"}[5m]))" | .panels[1].targets[0].legendFormat = "{{name}}"
        | .panels[2].title = "Memória dos frontends" | .panels[2].targets[0].expr = "sum by (name) (container_memory_working_set_bytes{name=~\".*(frontend|web).*\"})" | .panels[2].targets[0].legendFormat = "{{name}}"
        | .panels[3].title = "Logs reais de frontend" | .panels[3].targets[0].expr = "{job=\"docker-container\",host_name=\"tqi-platform\"} |= \"frontend\""
        | .panels[4].type = "stat" | .panels[4].title = "RUM/Web Vitals instrumentado" | .panels[4].datasource = {type:"prometheus",uid:"prometheus"} | .panels[4].targets = [{expr:"count(web_vitals_lcp_seconds)",refId:"A"}]
      ' "$dashboard" > "$tmp"
      ;;
    database)
      jq '
        .description = "Runtime real dos bancos em contêiner; o exporter PostgreSQL ainda depende de credencial monitor aprovada."
        | .templating.list |= map(if .name == "environment" then .query = "production" | .current = {text:"production", value:"production"} else . end)
        | .panels[0].title = "Bancos em contêiner observados" | .panels[0].targets[0].expr = "count(container_last_seen{name=~\".*(postgres|redis).*\"})"
        | .panels[1].title = "CPU dos bancos" | .panels[1].targets[0].expr = "sum by (name) (rate(container_cpu_usage_seconds_total{name=~\".*(postgres|redis).*\"}[5m]))" | .panels[1].targets[0].legendFormat = "{{name}}"
        | .panels[2].title = "Memória dos bancos" | .panels[2].targets[0].expr = "sum by (name) (container_memory_working_set_bytes{name=~\".*(postgres|redis).*\"})" | .panels[2].targets[0].legendFormat = "{{name}}"
        | .panels[3].title = "Logs reais dos bancos" | .panels[3].targets[0].expr = "{job=\"docker-container\",host_name=\"tqi-platform\"} |= \"postgres\""
        | .panels[4].type = "stat" | .panels[4].title = "Exporter PostgreSQL disponível" | .panels[4].datasource = {type:"prometheus",uid:"prometheus"} | .panels[4].targets = [{expr:"max(up{job=\"sentinel-postgres\"})",refId:"A"}]
      ' "$dashboard" > "$tmp"
      ;;
    absent-source)
      case "$name" in
        aws-overview.json) job=sentinel-aws; label="AWS" ;;
        continuous-profiling.json) job=sentinel-pyroscope-agent; label="continuous-profiling" ;;
        ecs-overview.json) job=sentinel-ecs; label="ECS" ;;
        kubernetes-overview.json) job=sentinel-kubernetes; label="Kubernetes" ;;
        messaging-overview.json) job=sentinel-messaging; label="mensageria" ;;
        vmware-overview.json) job=sentinel-vmware; label="VMware" ;;
      esac
      jq --arg job "$job" --arg label "$label" '
        .description = ("Sem telemetria de demonstração. Este painel mostra o estado real da integração " + $label + "; habilite o coletor antes de tratá-lo como cobertura.")
        | .templating.list |= map(if .name == "environment" then .query = "production" | .current = {text:"production", value:"production"} else . end)
        | .panels[] |= (.type = "stat" | .datasource = {type:"prometheus",uid:"prometheus"} | .targets = [{expr:("max(up{job=\"" + $job + "\"})"),refId:"A"}])
        | .panels[0].title = ("Coletor " + $label + " disponível")
        | .panels[1].title = "Sem fonte de dados real configurada"
        | .panels[2].title = "Onboarding obrigatório antes do uso operacional"
        | .panels[3].title = "Não há logs desta integração"
        | .panels[4].title = "Não há traces desta integração"
      ' "$dashboard" > "$tmp"
      ;;
  esac
  mv "$tmp" "$dashboard"

  case "$name" in
    apm.json|application-overview.json)
      tmp=$(mktemp)
      jq '
        .description = "APM real das aplicações Java instrumentadas no tqi-platform; não há métricas demo."
        | (.templating.list[] | select(.name == "service").query.query) = "query_result(label_replace(count by (job) (http_server_request_duration_seconds_count{job=~\"tqi-platform/.+\"}), \"service\", \"$1\", \"job\", \"(.*)\"))"
        | (.panels[] | select(.id == 5)) |= (.title = "Erros HTTP 5xx" | .targets = [{expr:"sum by (job) (rate(http_server_request_duration_seconds_count{job=~\"$service\",http_response_status_code=~\"5..\"}[5m]))",refId:"A"}])
        | (.panels[] | select(.id == 6)) |= (.title = "SLO de disponibilidade" | .targets = [{expr:"1 - sum(rate(http_server_request_duration_seconds_count{job=~\"$service\",http_response_status_code=~\"5..\"}[30m])) / clamp_min(sum(rate(http_server_request_duration_seconds_count{job=~\"$service\"}[30m])), 0.000001)",refId:"A"}])
        | (.panels[] | select(.id == 7)) |= (.targets = [{expr:"sum by (job,http_route) (rate(http_server_request_duration_seconds_count{job=~\"$service\"}[5m]))",legendFormat:"{{job}} {{http_route}}",refId:"A"}])
        | (.panels[] | select(.id == 8)) |= (.targets = [
            {expr:"histogram_quantile(0.50, sum by (le,job) (rate(http_server_request_duration_seconds_bucket{job=~\"$service\"}[5m])))",legendFormat:"p50 {{job}}",refId:"A"},
            {expr:"histogram_quantile(0.95, sum by (le,job) (rate(http_server_request_duration_seconds_bucket{job=~\"$service\"}[5m])))",legendFormat:"p95 {{job}}",refId:"B"},
            {expr:"histogram_quantile(0.99, sum by (le,job) (rate(http_server_request_duration_seconds_bucket{job=~\"$service\"}[5m])))",legendFormat:"p99 {{job}}",refId:"C"}
          ])
        | (.panels[] | select(.id == 9)) |= (.title = "Logs reais dos contêineres" | .targets = [{expr:"{job=\"docker-container\",host_name=\"tqi-platform\"}",refId:"A"}])
        | (.panels[] | select(.id == 10)) |= (.title = "Traces reais do tqi-platform" | .targets = [{query:"{ resource.service.namespace = \"tqi-platform\" }",queryType:"traceql",refId:"A"}])
        | (.panels[] | select(.id == 11)) |= (.title = "Continuous profiling pendente de agente real")
      ' "$dashboard" > "$tmp"
      mv "$tmp" "$dashboard"
      ;;
  esac
done
