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
    "noc-overview.json",
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
            "multi": True,
            "refresh": 1,
            "current": ALL,
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
                    "Aplicação / container",
                    PROMETHEUS,
                    'label_values(container_last_seen{deployment_environment=~"$environment",instance=~"$host",name!=""}, name)',
                ),
                query_variable(
                    "container_id",
                    "Container ID",
                    PROMETHEUS,
                    'label_values(container_last_seen{deployment_environment=~"$environment",instance=~"$host",name=~"$container"}, id)',
                    # Sem allValue customizado: o Grafana expande "All" apenas
                    # para os IDs retornados pelo container selecionado.
                    all_value=None,
                    regex='/.*\\/([a-f0-9]{12,64})$/',
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
            "Aplicação",
            PROMETHEUS,
            "label_values(http_server_request_duration_seconds_count, job)",
            all_value=".+",
            regex='/([^\\/"]+)$/',
        ),
        query_variable(
            "route",
            "Rota ativa (top 200)",
            PROMETHEUS,
            'query_result(topk(200, sum by (http_route) (rate(http_server_request_duration_seconds_count{job=~"(.*/)?$application",http_route!=""}[15m]))))',
            # Preserva o top-200 ao selecionar "All" em vez de usar .*, que
            # voltaria a casar todas as rotas de alta cardinalidade.
            all_value=None,
            regex='/http_route="([^"]+)"/',
        ),
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
        [prometheus_target(f"sum by (job, http_route) (rate({count_metric}[5m]))", legend="{{job}} · {{http_route}}")],
    )
    set_panel(
        panels,
        3,
        [
            prometheus_target(
                f"histogram_quantile(0.95, sum by (le, job, http_route) (rate({bucket_metric}[5m])))",
                legend="{{job}} · {{http_route}}",
            )
        ],
    )
    log_targets = [
        loki_target(
            '{job="docker-container",host_name=~"$host",filename=~".*/$container_id/.*",deployment_environment=~"$environment"}',
            "A",
        ),
        loki_target('{service_name=~"$application"}', "B"),
    ]
    set_panel(panels, 4, log_targets, datasource=LOKI, title="Logs filtrados da aplicação / container")

    if len(panels) <= 5:
        set_panel(
            panels,
            5,
            [trace_target('{ resource.service.name =~ "$application" && span.http.route =~ "$route" }')],
            datasource=TEMPO,
        )
        return

    set_panel(
        panels,
        5,
        [prometheus_target(f"sum by (job, http_route) (rate({error_metric}[5m]))", legend="{{job}} · {{http_route}}")],
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
        [prometheus_target(f"sum by (job, http_route) (rate({count_metric}[5m]))", legend="{{job}} · {{http_route}}")],
    )
    set_panel(
        panels,
        8,
        [
            prometheus_target(
                f"histogram_quantile(0.50, sum by (le, job, http_route) (rate({bucket_metric}[5m])))",
                "A",
                "p50 {{job}} · {{http_route}}",
            ),
            prometheus_target(
                f"histogram_quantile(0.95, sum by (le, job, http_route) (rate({bucket_metric}[5m])))",
                "B",
                "p95 {{job}} · {{http_route}}",
            ),
            prometheus_target(
                f"histogram_quantile(0.99, sum by (le, job, http_route) (rate({bucket_metric}[5m])))",
                "C",
                "p99 {{job}} · {{http_route}}",
            ),
        ],
    )
    set_panel(panels, 9, log_targets, datasource=LOKI, title="Logs filtrados da aplicação / container")
    set_panel(
        panels,
        10,
        [trace_target('{ resource.service.name =~ "$application" && span.http.route =~ "$route" }')],
        datasource=TEMPO,
    )
    set_panel(
        panels,
        11,
        [
            {
                "queryType": "profile",
                "profileTypeId": "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
                "labelSelector": '{service_name=~"$application"}',
                "refId": "A",
            }
        ],
        datasource=PYROSCOPE,
    )


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
        [loki_target('{job="docker-container",deployment_environment=~"$environment",host_name=~"$host",filename=~".*/$container_id/.*"}')],
        datasource=LOKI,
    )
    set_panel(panels, 5, [prometheus_target(f"count(container_last_seen{{{container}}})")], datasource=PROMETHEUS)


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
    dashboard["description"] = "Sondas HTTP reais filtráveis por grupo e alvo monitorado."
    panels = panels_by_id(dashboard)
    selector = 'job=~"$probe_job",instance=~"$target"'
    set_panel(panels, 1, [prometheus_target(f"min(probe_success{{{selector}}})")])
    set_panel(panels, 2, [prometheus_target(f"probe_duration_seconds{{{selector}}}", legend="{{instance}}")])
    set_panel(panels, 3, [prometheus_target(f"probe_dns_lookup_time_seconds{{{selector}}}", legend="{{instance}}")])
    set_panel(panels, 4, [prometheus_target(f"probe_success{{{selector}}}", legend="{{instance}}")], datasource=PROMETHEUS)
    set_panel(panels, 5, [prometheus_target(f"probe_http_status_code{{{selector}}}", legend="{{instance}}")], datasource=PROMETHEUS)


def apply_logs(dashboard: dict[str, object]) -> None:
    variables = infrastructure_variables()
    log_source = query_variable(
        "log_source",
        "Fonte de logs",
        LOKI,
        'label_values({job=~".+"}, job)',
        all_value=".+",
    )
    # O foco operacional padrão é a aplicação/container. As demais fontes
    # continuam disponíveis no seletor, inclusive a opção All.
    log_source["current"] = {
        "selected": True,
        "text": "docker-container",
        "value": "docker-container",
    }
    variables.insert(0, log_source)
    variables.append(
        query_variable(
            "stream",
            "Stream",
            LOKI,
            'label_values({job="docker-container",host_name=~"$host",filename=~".*/$container_id/.*"}, stream)',
        )
    )
    variables.append(textbox_variable("search", "Buscar no conteúdo (regex)"))
    dashboard["templating"] = {"list": variables}
    dashboard["description"] = (
        "Logs pesquisáveis por fonte, aplicação/container Docker, ambiente, host, stream e conteúdo."
    )
    panels = panels_by_id(dashboard)
    docker = (
        '{job=~"$log_source",deployment_environment=~"$environment",host_name=~"$host",'
        'filename=~".*/$container_id/.*",stream=~"$stream"} |~ "$search"'
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
            loki_target(f"sum by (host_name, filename) (count_over_time({docker} [$__interval]))", "A", "{{host_name}} · {{filename}}"),
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
        title="Erros por intervalo",
    )
    set_panel(
        panels,
        4,
        [loki_target(docker, "A"), loki_target(other, "B")],
        datasource=LOKI,
        title="Logs filtrados",
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
        title="Erros no período",
    )


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
    variables = [*control_variables(), *infrastructure_variables()]
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
                '{job="docker-container",deployment_environment=~"$environment",host_name=~"$host",filename=~".*/$container_id/.*"}'
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


def apply_frontend_or_database(dashboard: dict[str, object], *, database: bool) -> None:
    dashboard["templating"] = {"list": infrastructure_variables()}
    panels = panels_by_id(dashboard)
    name_pattern = ".*(postgres|redis).*" if database else ".*(frontend|web).*"
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
        [loki_target('{job="docker-container",deployment_environment=~"$environment",host_name=~"$host",filename=~".*/$container_id/.*"}')],
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
    panels = panels_by_id(dashboard)
    for panel_id in panels:
        set_panel(
            panels,
            panel_id,
            [prometheus_target(f'max(up{{job="{job}",instance=~"$collector_instance"}})')],
            datasource=PROMETHEUS,
        )


def apply_docker(dashboard: dict[str, object]) -> None:
    dashboard["templating"] = {"list": infrastructure_variables(include_container=True)[:-1]}
    dashboard["description"] = "Métricas Docker filtráveis por ambiente, host e aplicação/container."
    panels = panels_by_id(dashboard)
    selector = 'deployment_environment=~"$environment",instance=~"$host",name=~"$container"'
    set_panel(panels, 1, [prometheus_target(f"count(container_last_seen{{{selector}}})")])
    set_panel(
        panels,
        2,
        [prometheus_target(f"sum by (instance, name) (rate(container_cpu_usage_seconds_total{{{selector}}}[5m])) * 100", legend="{{instance}} · {{name}}")],
    )
    set_panel(
        panels,
        3,
        [prometheus_target(f"sum by (instance, name) (container_memory_working_set_bytes{{{selector}}})", legend="{{instance}} · {{name}}")],
    )
    set_panel(
        panels,
        4,
        [
            prometheus_target(f"sum by (instance, name) (rate(container_network_receive_bytes_total{{{selector}}}[5m]))", "A", "{{instance}} · {{name}} RX"),
            prometheus_target(f"sum by (instance, name) (rate(container_network_transmit_bytes_total{{{selector}}}[5m]))", "B", "{{instance}} · {{name}} TX"),
        ],
    )
    set_panel(
        panels,
        5,
        [
            prometheus_target(
                f"sum by (instance, name) (rate(container_fs_reads_bytes_total{{{selector}}}[5m]) + rate(container_fs_writes_bytes_total{{{selector}}}[5m]))",
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
    set_panel(panels, 4, [prometheus_target(f'100 * (1 - node_memory_MemAvailable_bytes{{{node}}} / node_memory_MemTotal_bytes{{{node}}})')])
    set_panel(
        panels,
        5,
        [prometheus_target(f'100 * (1 - node_filesystem_avail_bytes{{{node},fstype!~"tmpfs|overlay"}} / node_filesystem_size_bytes{{{node},fstype!~"tmpfs|overlay"}})')],
    )
    selected_logs = '{job="docker-container",deployment_environment=~"$environment",host_name=~"$host",filename=~".*/$container_id/.*"}'
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
    set_panel(panels, 11, [prometheus_target(f"sum by (job, http_request_method, http_route) ({count_rate})", legend="{{job}} · {{http_request_method}} {{http_route}}")])
    set_panel(panels, 12, [prometheus_target(f"histogram_quantile(0.95, sum by (le, job, http_route) ({bucket_rate}))", legend="{{job}} · {{http_route}}")])
    set_panel(
        panels,
        13,
        [prometheus_target(f"100 * sum by (job) ({error_rate}) / clamp_min(sum by (job) ({count_rate}), 0.000001)", legend="{{job}}")],
    )
    set_panel(
        panels,
        14,
        [trace_target('{ resource.service.namespace =~ "$host" && resource.service.name =~ "$application" && span.http.route =~ "$route" }')],
        datasource=TEMPO,
    )
    set_panel(
        panels,
        15,
        [loki_target(selected_logs, "A"), loki_target('{service_name=~"$application"}', "B")],
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


def apply_filters(path: Path) -> None:
    dashboard = json.loads(path.read_text(encoding="utf-8"))
    name = path.name
    if name in APPLICATION_DASHBOARDS:
        apply_application(dashboard)
    elif name in HOST_DASHBOARDS:
        apply_host(dashboard)
    elif name in SYNTHETIC_DASHBOARDS:
        apply_synthetic(dashboard)
    elif name == "logs.json":
        apply_logs(dashboard)
    elif name in {"alerts.json", "sentinelops-self-monitoring.json"}:
        apply_control(dashboard)
    elif name == "cardinality-ingestion-cost.json":
        apply_control(dashboard, cardinality=True)
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
