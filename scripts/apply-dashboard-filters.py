#!/usr/bin/env python3
"""Apply production-safe, datasource-aware filters to managed dashboards."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


PROMETHEUS = {"type": "prometheus", "uid": "prometheus"}
LOKI = {"type": "loki", "uid": "loki"}
TEMPO = {"type": "tempo", "uid": "tempo"}
PYROSCOPE = {"type": "grafana-pyroscope-datasource", "uid": "pyroscope"}
ALL = {"selected": True, "text": "All", "value": "$__all"}


APPLICATION_DASHBOARDS = {
    "apm.json",
    "application-overview.json",
    "distributed-tracing.json",
    "errors.json",
    "latency.json",
    "service-graph.json",
    "service-health.json",
    "slo-error-budget.json",
    "throughput.json",
}
HOST_DASHBOARDS = {
    "agent-fleet.json",
    "azure-overview.json",
    "capacity-forecast.json",
    "executive-overview.json",
}
SYNTHETIC_DASHBOARDS = {
    "api-test-results.json",
    "browser-test-results.json",
    "incident-timeline.json",
    "release-comparison.json",
    "release-validation.json",
    "synthetic-monitoring.json",
}
ABSENT_SOURCE_JOBS = {
    "aws-overview.json": "sentinel-aws",
    "continuous-profiling.json": "sentinel-pyroscope-agent",
    "ecs-overview.json": "sentinel-ecs",
    "kubernetes-overview.json": "sentinel-kubernetes",
    "messaging-overview.json": "sentinel-messaging",
    "vmware-overview.json": "sentinel-vmware",
}


def query_variable(
    name: str,
    label: str,
    datasource: dict[str, str],
    query: str,
    *,
    all_value: str | None = ".*",
    regex: str | None = None,
    hide: int = 0,
    current_text: str = "All",
    current_value: str = "$__all",
    multi: bool = True,
) -> dict[str, object]:
    variable: dict[str, object] = {
        "name": name,
        "label": label,
        "type": "query",
        "datasource": datasource,
        "query": {"query": query, "refId": f"var-{name}"},
        "includeAll": True,
    }
    if all_value is not None:
        variable["allValue"] = all_value
    variable.update(
        {
            "multi": multi,
            "refresh": 1,
            "current": {
                "selected": True,
                "text": current_text,
                "value": current_value,
            },
            "hide": hide,
        }
    )
    if regex:
        variable["regex"] = regex
    return variable


def textbox_variable(name: str, label: str) -> dict[str, object]:
    return {
        "name": name,
        "label": label,
        "type": "textbox",
        "query": ".*",
        "current": {"selected": True, "text": ".*", "value": ".*"},
    }


def custom_variable(
    name: str,
    label: str,
    values: str,
    *,
    current_text: str,
    current_value: str,
    all_value: str = ".+",
) -> dict[str, object]:
    return {
        "name": name,
        "label": label,
        "type": "custom",
        "query": values,
        "includeAll": True,
        "allValue": all_value,
        "multi": True,
        "current": {
            "selected": True,
            "text": current_text,
            "value": current_value,
        },
        "options": [],
    }


def prometheus_target(expr: str, ref_id: str = "A", legend: str | None = None) -> dict[str, object]:
    target: dict[str, object] = {"expr": expr, "refId": ref_id}
    if legend:
        target["legendFormat"] = legend
    return target


def loki_target(expr: str, ref_id: str = "A", legend: str | None = None) -> dict[str, object]:
    target: dict[str, object] = {"expr": expr, "refId": ref_id}
    if legend:
        target["legendFormat"] = legend
    return target


def trace_target(query: str, ref_id: str = "A") -> dict[str, object]:
    return {"query": query, "queryType": "traceql", "refId": ref_id}


def panels_by_id(dashboard: dict[str, object]) -> dict[int, dict[str, object]]:
    return {panel["id"]: panel for panel in dashboard.get("panels", []) if "id" in panel}


def set_panel(
    panels: dict[int, dict[str, object]],
    panel_id: int,
    targets: list[dict[str, object]],
    *,
    datasource: dict[str, str] | None = None,
    title: str | None = None,
    panel_type: str | None = None,
) -> None:
    panel = panels.get(panel_id)
    if panel is None:
        return
    panel["targets"] = targets
    if datasource:
        panel["datasource"] = datasource
    if title:
        panel["title"] = title
    if panel_type:
        panel["type"] = panel_type


def infrastructure_variables(*, include_container: bool = True) -> list[dict[str, object]]:
    variables = [
        query_variable(
            "environment",
            "Ambiente",
            PROMETHEUS,
            'label_values(up{job="linux-node"}, deployment_environment)',
            current_text="production",
            current_value="production",
            multi=False,
        ),
        query_variable(
            "host",
            "Host",
            PROMETHEUS,
            'label_values(node_uname_info{deployment_environment=~"$environment"}, instance)',
        ),
    ]
    if include_container:
        variables.extend(
            [
                query_variable(
                    "container",
                    "Container Docker",
                    PROMETHEUS,
                    'label_values(container_last_seen{deployment_environment=~"$environment",instance=~"$host",name!=""}, name)',
                    all_value=".+",
                ),
                query_variable(
                    "container_id",
                    "Container ID",
                    PROMETHEUS,
                    'label_values(container_last_seen{deployment_environment=~"$environment",instance=~"$host",name=~"$container"}, id)',
                    # Sem allValue customizado: o Grafana expande "All" apenas
                    # para os IDs retornados pelo container selecionado.
                    all_value=None,
                    regex='/.*(?:\\/|docker-)([a-f0-9]{64})(?:\\.scope)?$/',
                    hide=2,
                ),
            ]
        )
    return variables


def application_variables(*, host_filters_apm: bool = False) -> list[dict[str, object]]:
    infrastructure = infrastructure_variables()
    if not host_filters_apm:
        infrastructure[1]["label"] = "Host (logs/containers)"
    return [
        query_variable(
            "application",
            "Aplicação APM (service.name)",
            PROMETHEUS,
            "label_values(http_server_request_duration_seconds_count, job)",
            all_value=".+",
        ),
        textbox_variable("route", "Rota APM (regex; padrão: todas)"),
        *infrastructure,
    ]


def apply_application(dashboard: dict[str, object]) -> None:
    dashboard["templating"] = {"list": application_variables()}
    dashboard["description"] = (
        "Telemetria real com filtros encadeados de aplicação, rota, ambiente, host e container."
    )
    panels = panels_by_id(dashboard)
    selector = 'job=~"(.*/)?$application",http_route=~"$route"'
    count_metric = f"http_server_request_duration_seconds_count{{{selector}}}"
    bucket_metric = f"http_server_request_duration_seconds_bucket{{{selector}}}"
    error_metric = (
        'http_server_request_duration_seconds_count{job=~"(.*/)?$application",'
        'http_route=~"$route",http_response_status_code=~"5.."}'
    )
    set_panel(
        panels,
        1,
        [prometheus_target(f"count(count by (job) ({count_metric}))")],
        title="Aplicações selecionadas com métricas",
    )
    set_panel(
        panels,
        2,
        [prometheus_target(f"sum(rate({count_metric}[5m]))")],
    )
    set_panel(
        panels,
        3,
        [
            prometheus_target(
                f"histogram_quantile(0.95, sum by (le) (rate({bucket_metric}[5m])))",
            )
        ],
    )
    application_logs = (
        '{job="docker-container",filename=~".*/(${container_id:regex})/.*",host_name=~"$host",'
        'deployment_environment=~"$environment"}'
    )
    set_panel(
        panels,
        4,
        [loki_target(f"sum(count_over_time({application_logs} [5m]))")],
        datasource=LOKI,
        title="Logs da aplicação nos últimos 5 minutos",
    )

    if len(panels) <= 5:
        set_panel(
            panels,
            5,
            [trace_target('{ resource.service.name =~ "$application" }')],
            datasource=TEMPO,
            title="Traces da aplicação selecionada",
        )
        return

    set_panel(
        panels,
        5,
        [prometheus_target(f"sum(rate({error_metric}[5m]))")],
        title="Erros HTTP 5xx por segundo",
    )
    set_panel(
        panels,
        6,
        [
            prometheus_target(
                f"1 - sum(rate({error_metric}[30m])) / clamp_min(sum(rate({count_metric}[30m])), 0.000001)"
            )
        ],
    )
    set_panel(
        panels,
        7,
        [prometheus_target(f"topk(10, (sum by (job, http_route) (rate({count_metric}[5m]))) > 0)", legend="{{job}} · {{http_route}}")],
    )
    set_panel(
        panels,
        8,
        [
            prometheus_target(
                f"histogram_quantile(0.50, sum by (le, job) (rate({bucket_metric}[5m])))",
                "A",
                "p50 · {{job}}",
            ),
            prometheus_target(
                f"histogram_quantile(0.95, sum by (le, job) (rate({bucket_metric}[5m])))",
                "B",
                "p95 · {{job}}",
            ),
            prometheus_target(
                f"histogram_quantile(0.99, sum by (le, job) (rate({bucket_metric}[5m])))",
                "C",
                "p99 · {{job}}",
            ),
        ],
    )
    set_panel(panels, 9, [loki_target(application_logs)], datasource=LOKI, title="Logs da aplicação selecionada")
    set_panel(
        panels,
        10,
        [trace_target('{ resource.service.name =~ "$application" }')],
        datasource=TEMPO,
        title="Traces da aplicação selecionada",
    )
    set_panel(
        panels,
        11,
        [prometheus_target('max(up{job="sentinel-pyroscope-agent"})')],
        datasource=PROMETHEUS,
        title="Profiling: agente real disponível",
        panel_type="stat",
    )


def specialize_application(dashboard: dict[str, object], name: str) -> None:
    """Give each application dashboard a real operational purpose."""
    if name in {"apm.json", "application-overview.json"}:
        return
    panels = panels_by_id(dashboard)
    selector = 'job=~"$application",http_route=~"$route"'
    count = f'http_server_request_duration_seconds_count{{{selector}}}'
    bucket = f'http_server_request_duration_seconds_bucket{{{selector}}}'
    errors = (
        'http_server_request_duration_seconds_count{job=~"$application",'
        'http_route=~"$route",http_response_status_code=~"5.."}'
    )

    if name == "errors.json":
        set_panel(panels, 1, [prometheus_target(f"sum(rate({errors}[5m]))")], title="Erros HTTP 5xx por segundo")
        set_panel(panels, 2, [prometheus_target(f"topk(10, sum by (job, http_route) (rate({errors}[5m])))", legend="{{job}} · {{http_route}}")], title="Top 10 rotas por erros 5xx")
        set_panel(panels, 3, [prometheus_target(f"topk(10, 100 * sum by (job) (rate({errors}[5m])) / clamp_min(sum by (job) (rate({count}[5m])), 0.000001))", legend="{{job}}")], title="Taxa de erro por aplicação")
        set_panel(panels, 4, [loki_target('{job="docker-container",filename=~".*/(${container_id:regex})/.*",host_name=~"$host",deployment_environment=~"$environment"} |~ "(?i)error|exception|fatal|panic"')], datasource=LOKI, title="Logs de erro dos containers selecionados", panel_type="logs")
        set_panel(panels, 5, [trace_target('{ resource.service.name =~ "$application" && status = error }')], datasource=TEMPO, title="Traces com erro")
    elif name == "latency.json":
        set_panel(panels, 1, [prometheus_target(f"histogram_quantile(0.95, sum by (le) (rate({bucket}[5m])))")], title="Latência p95")
        set_panel(panels, 2, [prometheus_target(f"topk(10, histogram_quantile(0.95, sum by (le, job, http_route) (rate({bucket}[5m]))))", legend="{{job}} · {{http_route}}")], title="Top 10 rotas mais lentas")
        set_panel(panels, 3, [
            prometheus_target(f"histogram_quantile(0.50, sum by (le) (rate({bucket}[5m])))", "A", "p50"),
            prometheus_target(f"histogram_quantile(0.95, sum by (le) (rate({bucket}[5m])))", "B", "p95"),
            prometheus_target(f"histogram_quantile(0.99, sum by (le) (rate({bucket}[5m])))", "C", "p99"),
        ], title="Tendência de latência")
        set_panel(panels, 5, [trace_target('{ resource.service.name =~ "$application" && duration > 1s }')], datasource=TEMPO, title="Traces acima de 1 segundo")
    elif name == "throughput.json":
        set_panel(panels, 1, [prometheus_target(f"sum(rate({count}[5m]))")], title="Requisições por segundo")
        set_panel(panels, 2, [prometheus_target(f"topk(10, sum by (job, http_route) (rate({count}[5m])))", legend="{{job}} · {{http_route}}")], title="Top 10 rotas por volume")
        set_panel(panels, 3, [prometheus_target(f"sum by (job) (rate({count}[5m]))", legend="{{job}}")], title="Throughput por aplicação")
    elif name == "slo-error-budget.json":
        availability = f"1 - sum(rate({errors}[30m])) / clamp_min(sum(rate({count}[30m])), 0.000001)"
        set_panel(panels, 1, [prometheus_target(availability)], title="Disponibilidade HTTP (30 min)")
        set_panel(panels, 2, [prometheus_target(f"bottomk(10, 1 - sum by (job) (rate({errors}[30m])) / clamp_min(sum by (job) (rate({count}[30m])), 0.000001))", legend="{{job}}")], title="Aplicações com menor disponibilidade")
        set_panel(panels, 3, [prometheus_target(f"100 * sum(rate({errors}[5m])) / clamp_min(sum(rate({count}[5m])), 0.000001)", legend="5xx")], title="Taxa de erro consumindo o SLO")
    elif name == "service-graph.json":
        dashboard["templating"]["list"] = [
            variable for variable in dashboard["templating"]["list"] if variable.get("name") != "route"
        ]
        set_panel(panels, 1, [prometheus_target('count(count by (client, server) (traces_service_graph_request_total))')], title="Dependências observadas")
        set_panel(panels, 2, [prometheus_target('topk(10, sum by (client, server) (rate(traces_service_graph_request_total[5m])))', legend="{{client}} → {{server}}")], title="Top dependências por volume")
        set_panel(panels, 3, [prometheus_target('topk(10, histogram_quantile(0.95, sum by (le, client, server) (rate(traces_service_graph_request_server_seconds_bucket[5m]))))', legend="{{client}} → {{server}}")], title="Latência p95 entre serviços")
        dashboard["description"] = "Grafo derivado exclusivamente de span metrics reais do Tempo; Sem dados indica metrics-generator não onboardado."
    elif name == "distributed-tracing.json":
        set_panel(panels, 1, [prometheus_target(f"count(count by (job) ({count}))")], title="Serviços com tráfego rastreável")
        set_panel(panels, 2, [prometheus_target(f"topk(10, sum by (job) (rate({count}[5m])))", legend="{{job}}")], title="Top serviços por tráfego")
        set_panel(panels, 3, [prometheus_target(f"histogram_quantile(0.95, sum by (le, job) (rate({bucket}[5m])))", legend="{{job}}")], title="Latência p95 dos serviços")
    elif name == "service-health.json":
        set_panel(panels, 1, [prometheus_target(f"count(count by (job) ({count}))")], title="Aplicações com tráfego")
        set_panel(panels, 2, [prometheus_target(f"topk(10, sum by (job) (rate({errors}[5m])))", legend="{{job}}")], title="Top aplicações por erro 5xx")
        set_panel(panels, 3, [prometheus_target(f"histogram_quantile(0.95, sum by (le, job) (rate({bucket}[5m])))", legend="{{job}}")], title="Latência p95 por aplicação")


def apply_host(dashboard: dict[str, object]) -> None:
    dashboard["templating"] = {"list": infrastructure_variables()}
    dashboard["description"] = (
        "Frota Linux e Docker com filtros encadeados de ambiente, host e aplicação/container."
    )
    panels = panels_by_id(dashboard)
    node = 'job="linux-node",deployment_environment=~"$environment",instance=~"$host"'
    container = 'deployment_environment=~"$environment",instance=~"$host",name=~"$container"'
    set_panel(panels, 1, [prometheus_target(f"count(up{{{node}}} == 1)")])
    set_panel(
        panels,
        2,
        [prometheus_target(f"100 - avg by (instance) (rate(node_cpu_seconds_total{{{node},mode=\"idle\"}}[5m])) * 100", legend="{{instance}}")],
    )
    set_panel(
        panels,
        3,
        [prometheus_target(f"100 * (1 - node_memory_MemAvailable_bytes{{{node}}} / node_memory_MemTotal_bytes{{{node}}})", legend="{{instance}}")],
    )
    set_panel(
        panels,
        4,
        [loki_target('{job="docker-container",deployment_environment=~"$environment",host_name=~"$host",filename=~".*/(${container_id:regex})/.*"}')],
        datasource=LOKI,
    )
    set_panel(panels, 5, [prometheus_target(f"count(container_last_seen{{{container}}})")], datasource=PROMETHEUS)


def specialize_host(dashboard: dict[str, object], name: str) -> None:
    panels = panels_by_id(dashboard)
    node = 'job="linux-node",deployment_environment=~"$environment",instance=~"$host"'
    container = 'deployment_environment=~"$environment",instance=~"$host",name!="",name=~"$container"'
    if name == "capacity-forecast.json":
        dashboard["templating"]["list"] = [v for v in dashboard["templating"]["list"] if v.get("name") != "container_id"]
        set_panel(panels, 1, [prometheus_target(f"count(container_last_seen{{{container}}})")], title="Containers nomeados observados")
        set_panel(panels, 2, [prometheus_target(f'100 - avg by (instance) (rate(node_cpu_seconds_total{{{node},mode="idle"}}[5m])) * 100', legend="{{instance}}")], title="CPU por host")
        set_panel(panels, 3, [prometheus_target(f'100 * (1 - node_memory_MemAvailable_bytes{{{node}}} / node_memory_MemTotal_bytes{{{node}}})', legend="{{instance}}")], title="Memória por host")
        set_panel(panels, 4, [prometheus_target(f'100 * (1 - node_filesystem_avail_bytes{{{node},fstype!~"tmpfs|overlay|squashfs"}} / node_filesystem_size_bytes{{{node},fstype!~"tmpfs|overlay|squashfs"}})', legend="{{instance}} · {{mountpoint}}")], datasource=PROMETHEUS, title="Filesystem por host", panel_type="timeseries")
        set_panel(panels, 5, [prometheus_target(f'count(up{{{node}}} == 1)')], title="Hosts disponíveis")
        dashboard["description"] = "Capacidade real de CPU, memória, filesystem e containers; tendências não são projeções sem modelo aprovado."
    elif name == "executive-overview.json":
        dashboard["templating"]["list"] = [v for v in dashboard["templating"]["list"] if v.get("name") not in {"container", "container_id"}]
        set_panel(panels, 1, [prometheus_target(f'count(up{{{node}}} == 1)')], title="Hosts disponíveis")
        set_panel(panels, 2, [prometheus_target(f'max(100 - avg by (instance) (rate(node_cpu_seconds_total{{{node},mode="idle"}}[5m])) * 100)')], title="Maior uso de CPU", panel_type="stat")
        set_panel(panels, 3, [prometheus_target(f'max(100 * (1 - node_memory_MemAvailable_bytes{{{node}}} / node_memory_MemTotal_bytes{{{node}}}))')], title="Maior uso de memória", panel_type="stat")
        set_panel(panels, 4, [prometheus_target('count(ALERTS{alertstate="firing"}) or vector(0)')], datasource=PROMETHEUS, title="Alertas ativos", panel_type="stat")
        set_panel(panels, 5, [prometheus_target('count(count by (job) (http_server_request_duration_seconds_count))')], title="Aplicações com APM")
        dashboard["description"] = "Resumo executivo de disponibilidade, saturação, alertas e cobertura APM com dados reais."
    elif name == "agent-fleet.json":
        dashboard["templating"]["list"] = [v for v in dashboard["templating"]["list"] if v.get("name") != "container_id"]
        set_panel(panels, 1, [prometheus_target(f'count(up{{{node}}} == 1)')], title="Collectors Linux disponíveis")
        set_panel(panels, 2, [prometheus_target(f'up{{{node}}}', legend="{{instance}}")], title="Estado dos collectors")
        set_panel(panels, 3, [prometheus_target(f'time() - timestamp(up{{{node}}})', legend="{{instance}}")], title="Idade do último scrape")
        set_panel(panels, 4, [loki_target('{job="linux-system",deployment_environment=~"$environment",host_name=~"$host"} |~ "(?i)alloy|cadvisor|beyla|sentinelops"')], datasource=LOKI, title="Logs dos collectors")
        set_panel(panels, 5, [prometheus_target(f'count(container_last_seen{{{container}}})')], title="Containers nomeados cobertos")
        dashboard["description"] = "Saúde, freshness e logs dos collectors instalados na frota Linux."
    elif name == "azure-overview.json":
        dashboard["title"] = "Azure — VMs Docker observadas"
        dashboard["description"] = "Visão das VMs Azure baseada nos collectors Linux reais; não representa inventário da API Azure."


def apply_noc(dashboard: dict[str, object]) -> None:
    """Build a failure-first landing page that a NOC operator can triage in <60s."""
    dashboard["templating"] = {"list": infrastructure_variables(include_container=False)}
    dashboard["description"] = (
        "Landing operacional failure-first: alertas e indisponibilidade nunca são ocultados "
        "pelos filtros; host e ambiente refinam somente capacidade e logs."
    )

    def panel(
        panel_id: int,
        title: str,
        panel_type: str,
        datasource: dict[str, str],
        targets: list[dict[str, object]],
        x: int,
        y: int,
        w: int,
        h: int,
        description: str,
    ) -> dict[str, object]:
        return {
            "id": panel_id,
            "title": title,
            "type": panel_type,
            "datasource": datasource,
            "targets": targets,
            "gridPos": {"x": x, "y": y, "w": w, "h": h},
            "description": description,
        }

    dashboard["panels"] = [
        panel(1, "Alertas ativos", "stat", PROMETHEUS,
              [prometheus_target('count(ALERTS{alertstate="firing"}) or vector(0)')],
              0, 0, 4, 4, "Todo alerta firing. Zero é o único estado saudável."),
        panel(2, "Alertas críticos", "stat", PROMETHEUS,
              [prometheus_target('count(ALERTS{alertstate="firing",severity="critical"}) or vector(0)')],
              4, 0, 4, 4, "Incidentes críticos que exigem atuação imediata."),
        panel(3, "Hosts sem coleta (esperado: 3)", "stat", PROMETHEUS,
              [prometheus_target('clamp_min(3 - count(up{job="linux-node"} == 1), 0)')],
              8, 0, 4, 4, "Gate fail-closed: diferença entre os três hosts esperados e os scrapes Linux ativos."),
        panel(4, "Maior uso de memória", "stat", PROMETHEUS,
              [prometheus_target('max(100 * (1 - node_memory_MemAvailable_bytes{job="linux-node",deployment_environment=~"$environment",instance=~"$host"} / node_memory_MemTotal_bytes{job="linux-node",deployment_environment=~"$environment",instance=~"$host"}))')],
              12, 0, 4, 4, "Pior utilização de memória entre os hosts selecionados."),
        panel(5, "Maior uso de filesystem", "stat", PROMETHEUS,
              [prometheus_target('max(100 * (1 - node_filesystem_avail_bytes{job="linux-node",deployment_environment=~"$environment",instance=~"$host",fstype!~"tmpfs|overlay|squashfs"} / node_filesystem_size_bytes{job="linux-node",deployment_environment=~"$environment",instance=~"$host",fstype!~"tmpfs|overlay|squashfs"}))')],
              16, 0, 4, 4, "Pior ocupação de filesystem persistente."),
        panel(6, "Taxa HTTP 5xx", "stat", PROMETHEUS,
              [prometheus_target('100 * sum(rate(http_server_request_duration_seconds_count{http_response_status_code=~"5.."}[5m])) / clamp_min(sum(rate(http_server_request_duration_seconds_count[5m])), 0.000001)')],
              20, 0, 4, 4, "Percentual global de respostas 5xx nos últimos 5 minutos."),
        panel(7, "Fila de atuação — alertas firing", "table", PROMETHEUS,
              [{"expr": 'ALERTS{alertstate="firing"}', "format": "table", "instant": True, "refId": "A"}],
              0, 4, 24, 7, "Priorize critical, depois warning. A tabela preserva alertname, severity e rótulos de origem."),
        panel(8, "Memória por host", "timeseries", PROMETHEUS,
              [prometheus_target('topk(5, 100 * (1 - node_memory_MemAvailable_bytes{job="linux-node",deployment_environment=~"$environment",instance=~"$host"} / node_memory_MemTotal_bytes{job="linux-node",deployment_environment=~"$environment",instance=~"$host"}))', legend="{{instance}}")],
              0, 11, 12, 7, "Top 5 hosts por utilização de memória; warning a partir de 80%."),
        panel(9, "Top aplicações por erro 5xx", "table", PROMETHEUS,
              [{"expr": 'topk(10, (100 * sum by (job) (rate(http_server_request_duration_seconds_count{http_response_status_code=~"5.."}[5m])) / clamp_min(sum by (job) (rate(http_server_request_duration_seconds_count[5m])), 0.000001)) > 0)', "format": "table", "instant": True, "refId": "A"}],
              12, 11, 12, 7, "Até 10 aplicações com maior taxa de 5xx; use a dashboard APM para investigar."),
        panel(10, "Volume de erros recentes por host", "timeseries", LOKI,
              [loki_target('topk(5, sum by (host_name) (count_over_time({job="docker-container",deployment_environment=~"$environment",host_name=~"$host"} |~ "(?i)error|exception|fatal|panic" [$__interval])))', legend="{{host_name}}")],
              0, 18, 24, 9, "Contagem sem conteúdo sensível. Investigue as linhas na dashboard Logs com acesso restrito."),
    ]
    dashboard["time"] = {"from": "now-1h", "to": "now"}
    dashboard["refresh"] = "30s"


def apply_synthetic(dashboard: dict[str, object]) -> None:
    dashboard["templating"] = {
        "list": [
            query_variable(
                "probe_job",
                "Grupo de sondas",
                PROMETHEUS,
                'label_values(probe_success{job=~"blackbox-.+"}, job)',
                all_value=".+",
            ),
            query_variable(
                "target",
                "Alvo",
                PROMETHEUS,
                'label_values(probe_success{job=~"$probe_job"}, instance)',
            ),
        ]
    }
    dashboard["description"] = (
        "Status de onboarding de sondas reais. Sem probe_success, não apresenta disponibilidade fabricada."
    )
    selector = 'job=~"$probe_job",instance=~"$target"'
    dashboard["panels"] = [
        {
            "id": 1,
            "type": "stat",
            "title": "Sonda real disponível",
            "description": "Pior estado no escopo. Sem dados significa que nenhuma sonda real foi onboardada.",
            "datasource": PROMETHEUS,
            "targets": [prometheus_target(f"min(probe_success{{{selector}}})")],
            "gridPos": {"x": 0, "y": 0, "w": 6, "h": 5},
        },
        {
            "id": 2,
            "type": "text",
            "title": "Onboarding de monitoramento sintético",
            "description": "Orientação operacional, sem métricas substitutas.",
            "options": {
                "mode": "markdown",
                "content": (
                    "### Sem sondas reais no escopo\n\n"
                    "Cadastre o alvo aprovado, valide TLS/DNS/HTTP e somente então habilite SLO e alertas. "
                    "A ausência de dados não é tratada como sucesso."
                ),
            },
            "gridPos": {"x": 6, "y": 0, "w": 18, "h": 5},
        },
    ]


def apply_logs(dashboard: dict[str, object]) -> None:
    variables = infrastructure_variables()
    log_source = custom_variable(
        "log_source",
        "Fonte de logs",
        "Aplicações Docker : docker-container,Sistema Linux : linux-system",
        current_text="Aplicações Docker",
        current_value="docker-container",
    )
    variables.insert(0, log_source)
    variables.append(
        query_variable(
            "stream",
            "Stream",
            LOKI,
            'label_values({job="docker-container",host_name=~"$host",filename=~".*/(${container_id:regex})/.*"}, stream)',
        )
    )
    variables.append(textbox_variable("search", "Buscar no conteúdo (regex)"))
    dashboard["templating"] = {"list": variables}
    dashboard["description"] = (
        "Logs pesquisáveis por fonte, container Docker, ambiente, host, stream e conteúdo. "
        "A detecção de erro é fallback por regex, não severidade normalizada; dados pessoais exigem redaction upstream."
    )
    panels = panels_by_id(dashboard)
    docker = (
        '{job=~"$log_source",deployment_environment=~"$environment",host_name=~"$host",'
        'filename=~".*/(${container_id:regex})/.*",stream=~"$stream"} |~ "$search"'
    )
    other = (
        '{job=~"$log_source",job!="docker-container",deployment_environment=~"$environment",'
        'host_name=~"$host"} |~ "$search"'
    )
    set_panel(
        panels,
        1,
        [
            loki_target(f"sum(count_over_time({docker} [$__range]))", "A"),
            loki_target(f"sum(count_over_time({other} [$__range]))", "B"),
        ],
        datasource=LOKI,
        title="Eventos no período",
    )
    set_panel(
        panels,
        2,
        [
            loki_target(f"sum by (host_name) (count_over_time({docker} [$__interval]))", "A", "{{host_name}}"),
            loki_target(f"sum by (job, host_name) (count_over_time({other} [$__interval]))", "B", "{{job}} · {{host_name}}"),
        ],
        datasource=LOKI,
        title="Volume de logs por aplicação/container",
    )
    set_panel(
        panels,
        3,
        [
            loki_target(f"sum(count_over_time({docker} |~ \"(?i)error|exception|fatal\" [$__interval]))", "A"),
            loki_target(f"sum(count_over_time({other} |~ \"(?i)error|exception|fatal\" [$__interval]))", "B"),
        ],
        datasource=LOKI,
        title="Possíveis erros por intervalo (regex)",
    )
    set_panel(
        panels,
        4,
        [loki_target(docker, "A"), loki_target(other, "B")],
        datasource=LOKI,
        title="Logs filtrados (máximo 200 linhas)",
        panel_type="logs",
    )
    set_panel(
        panels,
        5,
        [
            loki_target(f"sum(count_over_time({docker} |~ \"(?i)error|exception|fatal\" [$__range]))", "A"),
            loki_target(f"sum(count_over_time({other} |~ \"(?i)error|exception|fatal\" [$__range]))", "B"),
        ],
        datasource=LOKI,
        title="Possíveis erros no período (regex)",
    )
    if panels.get(3):
        panels[3]["description"] = "Triagem textual por error/exception/fatal; stack traces podem elevar a contagem."
    if panels.get(4):
        panels[4]["description"] = "Conteúdo bruto pode conter dados pessoais; aplicar mascaramento no collector antes da ingestão."
    if panels.get(5):
        panels[5]["description"] = "Fallback por regex; não equivale a uma contagem estruturada por severity."


def control_variables(*, include_alerts: bool = True) -> list[dict[str, object]]:
    variables = [
        query_variable(
            "prometheus_job",
            "Job Prometheus",
            PROMETHEUS,
            "label_values(up, job)",
            all_value=".+",
        ),
        query_variable(
            "prometheus_instance",
            "Instância Prometheus",
            PROMETHEUS,
            'label_values(up{job=~"$prometheus_job"}, instance)',
        ),
    ]
    if include_alerts:
        variables.extend(
            [
                query_variable("severity", "Severidade", PROMETHEUS, "label_values(ALERTS, severity)"),
                query_variable(
                    "alertname",
                    "Alerta",
                    PROMETHEUS,
                    'label_values(ALERTS{severity=~"$severity"}, alertname)',
                ),
            ]
        )
    return variables


def apply_control(dashboard: dict[str, object], *, cardinality: bool = False) -> None:
    variables = [*control_variables(include_alerts=not cardinality), *infrastructure_variables()]
    if cardinality:
        variables.insert(
            2,
            query_variable(
                "metric",
                "Métrica",
                PROMETHEUS,
                'label_values({job=~"$prometheus_job"}, __name__)',
                all_value=".+",
            ),
        )
    dashboard["templating"] = {"list": variables}
    dashboard["description"] = "Plano de controle filtrável por job, instância, severidade e alerta."
    panels = panels_by_id(dashboard)
    set_panel(
        panels,
        1,
        [prometheus_target('count(up{job=~"$prometheus_job",instance=~"$prometheus_instance"} == 1)')],
    )
    if cardinality:
        set_panel(
            panels,
            2,
            [prometheus_target('count({__name__=~"$metric",job=~"$prometheus_job"})')],
            title="Séries ativas no filtro",
        )
    set_panel(
        panels,
        4,
        [
            loki_target(
                '{job="docker-container",deployment_environment=~"$environment",host_name=~"$host",filename=~".*/(${container_id:regex})/.*"}'
            )
        ],
        datasource=LOKI,
        title="Logs filtrados da aplicação / container",
    )
    set_panel(
        panels,
        5,
        [prometheus_target('count(ALERTS{alertstate="firing",severity=~"$severity",alertname=~"$alertname"})')],
        datasource=PROMETHEUS,
    )


def specialize_control(dashboard: dict[str, object], name: str) -> None:
    panels = panels_by_id(dashboard)
    if name == "alerts.json":
        dashboard["templating"]["list"] = [
            variable
            for variable in dashboard["templating"]["list"]
            if variable.get("name") not in {"prometheus_job", "prometheus_instance"}
        ]
        selector = 'alertstate="firing",severity=~"$severity",alertname=~"$alertname"'
        set_panel(panels, 1, [prometheus_target(f"count(ALERTS{{{selector}}}) or vector(0)")], title="Alertas firing no filtro")
        set_panel(panels, 2, [prometheus_target(f"sum by (severity) (ALERTS{{{selector}}})", legend="{{severity}}")], title="Alertas por severidade")
        set_panel(panels, 3, [prometheus_target(f"topk(10, sum by (alertname) (ALERTS{{{selector}}}))", legend="{{alertname}}")], title="Top alertas ativos")
        set_panel(panels, 4, [loki_target('{job="docker-container",deployment_environment=~"$environment",host_name=~"$host",filename=~".*/(${container_id:regex})/.*"} |~ "(?i)error|exception|fatal|panic"')], datasource=LOKI, title="Logs de erro para correlação")
        set_panel(panels, 5, [prometheus_target('count(ALERTS{alertstate="firing",severity="critical"}) or vector(0)')], title="Alertas críticos")
        dashboard["description"] = "Fila de alertas firing por severidade e nome, com logs de aplicação para correlação."
    elif name == "cardinality-ingestion-cost.json":
        dashboard["templating"]["list"] = [
            v for v in dashboard["templating"]["list"]
            if v.get("name") in {"prometheus_job", "prometheus_instance", "metric"}
        ]
        set_panel(panels, 1, [prometheus_target('count(up{job=~"$prometheus_job",instance=~"$prometheus_instance"} == 1)')], title="Targets saudáveis")
        set_panel(panels, 3, [prometheus_target('rate(prometheus_tsdb_head_samples_appended_total[5m])', legend="amostras/s")], title="Ingestão de amostras")
        set_panel(panels, 4, [prometheus_target('topk(10, count by (__name__) ({__name__=~"$metric",job=~"$prometheus_job"}))', legend="{{__name__}}")], datasource=PROMETHEUS, title="Top 10 métricas por séries", panel_type="timeseries")
        set_panel(panels, 5, [prometheus_target('sum(scrape_samples_scraped{job=~"$prometheus_job",instance=~"$prometheus_instance"})')], title="Amostras por scrape")
        dashboard["description"] = "Custo técnico de cardinalidade e ingestão; top 10 limita fan-out visual e custo da consulta."


def apply_frontend_or_database(dashboard: dict[str, object], *, database: bool) -> None:
    dashboard["templating"] = {"list": infrastructure_variables()}
    panels = panels_by_id(dashboard)
    name_pattern = ".*(postgres|postgresql|redis|mysql|mariadb).*" if database else ".*(frontend|web).*"
    selector = (
        'deployment_environment=~"$environment",instance=~"$host",'
        f'name=~"$container",name=~"{name_pattern}"'
    )
    set_panel(panels, 1, [prometheus_target(f"count(container_last_seen{{{selector}}})")])
    set_panel(
        panels,
        2,
        [prometheus_target(f"sum by (instance, name) (rate(container_cpu_usage_seconds_total{{{selector}}}[5m]))", legend="{{instance}} · {{name}}")],
    )
    set_panel(
        panels,
        3,
        [prometheus_target(f"sum by (instance, name) (container_memory_working_set_bytes{{{selector}}})", legend="{{instance}} · {{name}}")],
    )
    set_panel(
        panels,
        4,
        [loki_target('{job="docker-container",deployment_environment=~"$environment",host_name=~"$host",filename=~".*/(${container_id:regex})/.*"}')],
        datasource=LOKI,
    )
    dashboard["description"] = (
        "Bancos filtráveis por ambiente, host e container."
        if database
        else "Frontends filtráveis por ambiente, host e aplicação/container."
    )


def apply_absent_source(dashboard: dict[str, object], job: str) -> None:
    dashboard["templating"] = {
        "list": [
            query_variable(
                "collector_instance",
                "Instância do collector",
                PROMETHEUS,
                f'label_values(up{{job="{job}"}}, instance)',
            )
        ]
    }
    integration = str(dashboard.get("title", "Integração"))
    dashboard["description"] = (
        f"Status de onboarding de {integration}. Não apresenta KPIs substitutos: sem collector real, exibe Sem dados."
    )
    dashboard["panels"] = [
        {
            "id": 1,
            "type": "stat",
            "title": f"Collector {integration}",
            "description": "1 = disponível; 0 = indisponível; Sem dados = integração ainda não cadastrada.",
            "datasource": PROMETHEUS,
            "targets": [prometheus_target(f'max(up{{job="{job}",instance=~"$collector_instance"}})')],
            "gridPos": {"x": 0, "y": 0, "w": 6, "h": 5},
        },
        {
            "id": 2,
            "type": "text",
            "title": "Integração pendente",
            "description": "Orientação operacional; não é dado de monitoramento.",
            "options": {
                "mode": "markdown",
                "content": (
                    "### Sem fonte real configurada\n\n"
                    "Esta dashboard não reutiliza métricas, logs ou traces de outro domínio. "
                    "Conclua o onboarding do collector e valide identidade, freshness e alertas "
                    "antes de habilitar o uso pelo NOC."
                ),
            },
            "gridPos": {"x": 6, "y": 0, "w": 18, "h": 5},
        },
    ]


def apply_docker(dashboard: dict[str, object]) -> None:
    dashboard["templating"] = {"list": infrastructure_variables(include_container=True)[:-1]}
    dashboard["description"] = "Métricas Docker filtráveis por ambiente, host e aplicação/container."
    panels = panels_by_id(dashboard)
    selector = 'deployment_environment=~"$environment",instance=~"$host",name!="",name=~"$container"'
    set_panel(panels, 1, [prometheus_target(f"count(container_last_seen{{{selector}}})")])
    set_panel(
        panels,
        2,
        [prometheus_target(f"topk(10, sum by (instance, name) (rate(container_cpu_usage_seconds_total{{{selector}}}[5m])) * 100)", legend="{{instance}} · {{name}}")],
    )
    set_panel(
        panels,
        3,
        [prometheus_target(f"topk(10, sum by (instance, name) (container_memory_working_set_bytes{{{selector}}}))", legend="{{instance}} · {{name}}")],
    )
    set_panel(
        panels,
        4,
        [
            prometheus_target(f"topk(10, sum by (instance, name) (rate(container_network_receive_bytes_total{{{selector}}}[5m])))", "A", "{{instance}} · {{name}} RX"),
            prometheus_target(f"topk(10, sum by (instance, name) (rate(container_network_transmit_bytes_total{{{selector}}}[5m])))", "B", "{{instance}} · {{name}} TX"),
        ],
    )
    set_panel(
        panels,
        5,
        [
            prometheus_target(
                f"topk(10, sum by (instance, name) (rate(container_fs_reads_bytes_total{{{selector}}}[5m]) + rate(container_fs_writes_bytes_total{{{selector}}}[5m])))",
                legend="{{instance}} · {{name}}",
            )
        ],
    )


def apply_linux(dashboard: dict[str, object]) -> None:
    variables = infrastructure_variables(include_container=False)
    variables.extend(
        [
            query_variable(
                "mountpoint",
                "Filesystem",
                PROMETHEUS,
                'label_values(node_filesystem_size_bytes{deployment_environment=~"$environment",instance=~"$host",fstype!~"tmpfs|overlay"}, mountpoint)',
            ),
            query_variable(
                "device",
                "Interface de rede",
                PROMETHEUS,
                'label_values(node_network_receive_bytes_total{deployment_environment=~"$environment",instance=~"$host",device!="lo"}, device)',
            ),
            textbox_variable("search", "Buscar nos logs (regex)"),
        ]
    )
    dashboard["templating"] = {"list": variables}
    panels = panels_by_id(dashboard)
    for panel in panels.values():
        for target in panel.get("targets", []):
            for field in ("expr", "query", "labelSelector"):
                if isinstance(target.get(field), str):
                    target[field] = target[field].replace("$instance", "$host")
    for panel_id in (5, 10):
        panel = panels.get(panel_id)
        if panel:
            for target in panel.get("targets", []):
                target["expr"] = target["expr"].replace('fstype!~"tmpfs|overlay"}', 'fstype!~"tmpfs|overlay",mountpoint=~"$mountpoint"}')
    panel = panels.get(9)
    if panel:
        for target in panel.get("targets", []):
            target["expr"] = re.sub(
                r'(?:device=~"\$device",)+device!="lo"',
                'device=~"$device",device!="lo"',
                target["expr"],
            )
            if 'device=~"$device"' not in target["expr"]:
                target["expr"] = target["expr"].replace(
                    'device!="lo"}', 'device=~"$device",device!="lo"}'
                )
    set_panel(
        panels,
        12,
        [loki_target('{job="linux-system",host_name=~"$host",deployment_environment=~"$environment"} |~ "$search"')],
        datasource=LOKI,
    )
    set_panel(
        panels,
        4,
        [prometheus_target('(max(1 - node_memory_MemAvailable_bytes{instance=~"$host",deployment_environment=~"$environment"} / node_memory_MemTotal_bytes{instance=~"$host",deployment_environment=~"$environment"})) * 100')],
    )


def apply_tqi(dashboard: dict[str, object]) -> None:
    dashboard["templating"] = {"list": application_variables(host_filters_apm=True)}
    dashboard["description"] = (
        "Visão TQI filtrável por aplicação, rota, ambiente, host e aplicação/container Docker."
    )
    panels = panels_by_id(dashboard)
    node = 'deployment_environment=~"$environment",instance=~"$host"'
    target_info = (
        'max by (job, instance, host_name) '
        '(target_info{host_name=~"$host",telemetry_sdk_name="beyla"})'
    )
    count_rate = (
        '(rate(http_server_request_duration_seconds_count{job=~"$host/(.*/)?$application",'
        'http_route=~"$route"}[5m]) or '
        '(rate(http_server_request_duration_seconds_count{job=~"(.*/)?$application",http_route=~"$route"}[5m]) '
        f'* on (job, instance) group_left (host_name) {target_info}))'
    )
    bucket_rate = (
        '(rate(http_server_request_duration_seconds_bucket{job=~"$host/(.*/)?$application",'
        'http_route=~"$route"}[5m]) or '
        '(rate(http_server_request_duration_seconds_bucket{job=~"(.*/)?$application",http_route=~"$route"}[5m]) '
        f'* on (job, instance) group_left (host_name) {target_info}))'
    )
    error_rate = (
        '(rate(http_server_request_duration_seconds_count{job=~"$host/(.*/)?$application",'
        'http_route=~"$route",http_response_status_code=~"5.."}[5m]) or '
        '(rate(http_server_request_duration_seconds_count{job=~"(.*/)?$application",http_route=~"$route",'
        'http_response_status_code=~"5.."}[5m]) '
        f'* on (job, instance) group_left (host_name) {target_info}))'
    )
    set_panel(panels, 2, [prometheus_target(f'count(up{{job="linux-node",{node}}} == 1)')])
    set_panel(panels, 3, [prometheus_target(f'100 - avg(rate(node_cpu_seconds_total{{{node},mode="idle"}}[5m])) * 100')])
    set_panel(panels, 4, [prometheus_target(f'max(100 * (1 - node_memory_MemAvailable_bytes{{{node}}} / node_memory_MemTotal_bytes{{{node}}}))')])
    set_panel(
        panels,
        5,
        [prometheus_target(f'max(100 * (1 - node_filesystem_avail_bytes{{{node},fstype!~"tmpfs|overlay"}} / node_filesystem_size_bytes{{{node},fstype!~"tmpfs|overlay"}}))')],
    )
    selected_logs = '{job="docker-container",deployment_environment=~"$environment",host_name=~"$host",filename=~".*/(${container_id:regex})/.*"}'
    set_panel(panels, 6, [loki_target(f'sum(count_over_time({selected_logs} |~ "(?i)error|exception|fatal" [15m]))')], datasource=LOKI)
    set_panel(panels, 7, [prometheus_target(f"count(sum by (job) ({count_rate}))")])
    set_panel(panels, 8, [prometheus_target(f'100 - avg by (instance) (rate(node_cpu_seconds_total{{{node},mode="idle"}}[5m])) * 100', legend="{{instance}}")])
    set_panel(panels, 9, [prometheus_target(f'100 * (1 - node_memory_MemAvailable_bytes{{{node}}} / node_memory_MemTotal_bytes{{{node}}})', legend="{{instance}}")])
    set_panel(
        panels,
        10,
        [
            prometheus_target(f'sum by (instance) (rate(node_network_receive_bytes_total{{{node},device!="lo"}}[5m]))', "A", "{{instance}} RX"),
            prometheus_target(f'sum by (instance) (rate(node_network_transmit_bytes_total{{{node},device!="lo"}}[5m]))', "B", "{{instance}} TX"),
        ],
    )
    set_panel(panels, 11, [prometheus_target(f"topk(10, (sum by (job, http_request_method, http_route) ({count_rate})) > 0)", legend="{{job}} · {{http_request_method}} {{http_route}}")])
    set_panel(panels, 12, [prometheus_target(f"topk(10, histogram_quantile(0.95, sum by (le, job, http_route) ({bucket_rate})))", legend="{{job}} · {{http_route}}")])
    set_panel(
        panels,
        13,
        [prometheus_target(f"100 * sum by (job) ({error_rate}) / clamp_min(sum by (job) ({count_rate}), 0.000001)", legend="{{job}}")],
    )
    set_panel(
        panels,
        14,
        [trace_target('{ resource.service.namespace =~ "$host" && resource.service.name =~ "$application" }')],
        datasource=TEMPO,
        title="APM — traces por aplicação e host",
    )
    set_panel(
        panels,
        15,
        [loki_target(selected_logs, "A")],
        datasource=LOKI,
    )


def apply_vmware_performance(dashboard: dict[str, object]) -> None:
    dashboard["templating"] = {
        "list": [
            query_variable(
                "endpoint",
                "Endpoint VMware",
                PROMETHEUS,
                "label_values(sentinelops_vmware_vm_power_state, endpoint)",
            ),
            query_variable(
                "vm",
                "Máquina virtual",
                PROMETHEUS,
                'label_values(sentinelops_vmware_vm_power_state{endpoint=~"$endpoint"}, vm_name)',
            ),
            query_variable(
                "datastore",
                "Datastore",
                PROMETHEUS,
                'label_values(sentinelops_vmware_datastore_capacity_bytes{endpoint=~"$endpoint"}, datastore)',
            ),
        ]
    }
    panels = panels_by_id(dashboard)
    set_panel(panels, 1, [prometheus_target('max(sentinelops_vmware_performance_scrape_success{endpoint=~"$endpoint"})')])
    set_panel(panels, 2, [prometheus_target('sum(sentinelops_vmware_vm_power_state{endpoint=~"$endpoint",vm_name=~"$vm"})')])
    set_panel(panels, 3, [prometheus_target('sum(sentinelops_vmware_vm_tools_running{endpoint=~"$endpoint",vm_name=~"$vm"})')])
    set_panel(panels, 4, [prometheus_target('sum(sentinelops_vmware_datastore_free_bytes{endpoint=~"$endpoint",datastore=~"$datastore"})')])
    vm_metrics = {
        5: [
            ("sentinelops_vmware_vm_network_receive_bytes_per_second", "RX"),
            ("sentinelops_vmware_vm_network_transmit_bytes_per_second", "TX"),
        ],
        6: [
            ("sentinelops_vmware_vm_disk_read_bytes_per_second", "leitura"),
            ("sentinelops_vmware_vm_disk_write_bytes_per_second", "escrita"),
        ],
        7: [
            ("sentinelops_vmware_vm_disk_read_iops", "leitura"),
            ("sentinelops_vmware_vm_disk_write_iops", "escrita"),
        ],
        8: [
            ("sentinelops_vmware_vm_disk_read_latency_milliseconds", "leitura"),
            ("sentinelops_vmware_vm_disk_write_latency_milliseconds", "escrita"),
        ],
        9: [("sentinelops_vmware_vm_cpu_usage_mhz", "")],
        10: [("sentinelops_vmware_vm_memory_usage_bytes", "")],
    }
    for panel_id, metrics in vm_metrics.items():
        targets = []
        for index, (metric, suffix) in enumerate(metrics):
            legend = "{{vm_name}}" + (f" {suffix}" if suffix else "")
            targets.append(
                prometheus_target(
                    f'{metric}{{endpoint=~"$endpoint",vm_name=~"$vm"}}',
                    chr(ord("A") + index),
                    legend,
                )
            )
        set_panel(panels, panel_id, targets)


def infer_unit(panel: dict[str, object]) -> tuple[str, int]:
    """Infer a conservative Grafana unit from query semantics, never from samples."""
    title = str(panel.get("title", "")).lower()
    query = " ".join(
        str(target.get("expr") or target.get("query") or "")
        for target in panel.get("targets", [])
    ).lower()
    if "status code" in title or "código http" in title:
        return "none", 0
    if "uptime" in title:
        return "s", 0
    if "requisições por segundo" in title or "por segundo" in title or "rps" in title:
        return "reqps", 2
    if "slo" in title or "disponibilidade" in title:
        return "percentunit", 2
    if "taxa" in title or "5xx" in title:
        return "percent", 1
    if "latência" in title or "latency" in title or "duração" in title:
        return "s", 3
    if "percent" in query or "100 *" in query or "* 100" in query or "utiliz" in title or "taxa" in title:
        return "percent", 1
    if "working_set_bytes" in query or ("memória" in title and "node_memory" not in query):
        return "bytes", 1
    if "bytes_per_second" in query or "bytes_total" in query or "rede" in title or "i/o" in title:
        return "Bps", 1
    return "short", 0


def thresholds_for(panel: dict[str, object]) -> dict[str, object]:
    title = str(panel.get("title", "")).lower()
    if "disponibilidade" in title or "slo" in title:
        steps = [
            {"color": "red", "value": None},
            {"color": "orange", "value": 0.99},
            {"color": "green", "value": 0.999},
        ]
    elif any(word in title for word in ("utiliz", "filesystem", "memória", "cpu")):
        steps = [
            {"color": "green", "value": None},
            {"color": "yellow", "value": 70},
            {"color": "orange", "value": 80},
            {"color": "red", "value": 90},
        ]
    elif any(word in title for word in ("alert", "erro", "indispon", "5xx", "sem coleta")):
        steps = [{"color": "green", "value": None}, {"color": "red", "value": 1}]
    elif any(word in title for word in ("collector", "coleta", "sucesso", "disponível")):
        steps = [{"color": "red", "value": None}, {"color": "green", "value": 1}]
    else:
        steps = [{"color": "green", "value": None}]
    return {"mode": "absolute", "steps": steps}


def apply_noc_ux(dashboard: dict[str, object], name: str) -> None:
    """Normalize readability and encode an explicit operational contract."""
    dashboard.setdefault("description", "")
    if not dashboard["description"]:
        dashboard["description"] = "Dashboard operacional baseada exclusivamente em fontes reais do SentinelOps."
    dashboard["graphTooltip"] = 1
    dashboard["refresh"] = dashboard.get("refresh") or "30s"
    dashboard["time"] = dashboard.get("time") or {"from": "now-1h", "to": "now"}
    dashboard["tags"] = sorted(set(dashboard.get("tags", [])) | {"sentinelops", "managed", "noc-ready"})

    for panel in dashboard.get("panels", []):
        panel_type = panel.get("type")
        title = str(panel.get("title", "Painel operacional"))
        panel.setdefault(
            "description",
            f"{title}. Exibe apenas telemetria real no escopo dos filtros; ausência é apresentada como Sem dados.",
        )
        if not panel.get("description"):
            panel["description"] = (
                f"{title}. Exibe apenas telemetria real no escopo dos filtros; "
                "ausência é apresentada como Sem dados."
            )

        for target in panel.get("targets", []):
            if panel_type == "logs":
                target["maxLines"] = 200
            elif panel_type == "timeseries":
                target["maxDataPoints"] = 600

        if panel_type in {"stat", "timeseries"}:
            unit, decimals = infer_unit(panel)
            defaults = panel.setdefault("fieldConfig", {}).setdefault("defaults", {})
            defaults["unit"] = unit
            defaults["decimals"] = decimals
            defaults["noValue"] = "Sem dados"
            defaults.setdefault("color", {"mode": "thresholds"})
            defaults["thresholds"] = thresholds_for(panel)
            panel["fieldConfig"].setdefault("overrides", [])

        if panel_type == "stat":
            panel["options"] = {
                "reduceOptions": {"values": False, "calcs": ["lastNotNull"], "fields": ""},
                "orientation": "auto",
                "textMode": "value",
                "wideLayout": True,
                "colorMode": "background",
                "graphMode": "none",
                "justifyMode": "auto",
                "showPercentChange": False,
            }
        elif panel_type == "timeseries":
            defaults = panel["fieldConfig"]["defaults"]
            defaults["custom"] = {
                "drawStyle": "line",
                "lineInterpolation": "linear",
                "lineWidth": 2,
                "fillOpacity": 8,
                "gradientMode": "none",
                "spanNulls": False,
                "showPoints": "never",
                "axisPlacement": "auto",
                "axisColorMode": "text",
                "scaleDistribution": {"type": "linear"},
                "thresholdsStyle": {"mode": "off"},
            }
            panel["options"] = {
                "legend": {
                    "displayMode": "table",
                    "placement": "bottom",
                    "calcs": ["lastNotNull", "max"],
                    "showLegend": True,
                },
                "tooltip": {"mode": "multi", "sort": "desc", "hideZeros": False},
            }
        elif panel_type == "logs":
            panel["options"] = {
                "showTime": True,
                "showLabels": False,
                "showCommonLabels": False,
                "wrapLogMessage": True,
                "prettifyLogMessage": False,
                "enableLogDetails": True,
                "sortOrder": "Descending",
                "dedupStrategy": "none",
            }
        elif panel_type == "table":
            table_fields = panel.setdefault("fieldConfig", {})
            table_fields.setdefault("defaults", {})["noValue"] = "Sem dados"
            table_fields.setdefault("overrides", [])
            panel["options"] = {
                "showHeader": True,
                "cellHeight": "sm",
                "sortBy": [],
                "footer": {"show": False, "reducer": ["sum"], "countRows": False, "fields": ""},
            }

    # Detail pages may expose All, but their visual fan-out is bounded. The
    # failure-first NOC page intentionally remains estate-wide.
    if name in APPLICATION_DASHBOARDS or name == "tqi-hosts-apm.json":
        dashboard["time"] = {"from": "now-1h", "to": "now"}


def apply_filters(path: Path) -> None:
    dashboard = json.loads(path.read_text(encoding="utf-8"))
    name = path.name
    if name in APPLICATION_DASHBOARDS:
        apply_application(dashboard)
        specialize_application(dashboard, name)
    elif name == "noc-overview.json":
        apply_noc(dashboard)
    elif name in HOST_DASHBOARDS:
        apply_host(dashboard)
        specialize_host(dashboard, name)
    elif name in SYNTHETIC_DASHBOARDS:
        apply_synthetic(dashboard)
    elif name == "logs.json":
        apply_logs(dashboard)
    elif name in {"alerts.json", "sentinelops-self-monitoring.json"}:
        apply_control(dashboard)
        specialize_control(dashboard, name)
    elif name == "cardinality-ingestion-cost.json":
        apply_control(dashboard, cardinality=True)
        specialize_control(dashboard, name)
    elif name in {"frontend-observability.json", "web-vitals.json"}:
        apply_frontend_or_database(dashboard, database=False)
    elif name == "database-overview.json":
        apply_frontend_or_database(dashboard, database=True)
    elif name in ABSENT_SOURCE_JOBS:
        apply_absent_source(dashboard, ABSENT_SOURCE_JOBS[name])
    elif name == "docker-overview.json":
        apply_docker(dashboard)
    elif name == "linux-overview.json":
        apply_linux(dashboard)
    elif name == "tqi-hosts-apm.json":
        apply_tqi(dashboard)
    elif name == "vmware-vm-performance.json":
        apply_vmware_performance(dashboard)
    else:
        raise ValueError(f"dashboard sem política de filtros: {path}")

    apply_noc_ux(dashboard, name)
    dashboard["version"] = 3
    path.write_text(json.dumps(dashboard, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> int:
    if len(sys.argv) != 2:
        print("uso: apply-dashboard-filters.py <diretório-dashboards>", file=sys.stderr)
        return 2
    dashboard_dir = Path(sys.argv[1])
    dashboard_paths = sorted(dashboard_dir.glob("*.json"))
    if not dashboard_paths:
        print(f"nenhuma dashboard encontrada em {dashboard_dir}", file=sys.stderr)
        return 2
    for dashboard_path in dashboard_paths:
        apply_filters(dashboard_path)
    print(f"Filtros aplicados em {len(dashboard_paths)} dashboards.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
