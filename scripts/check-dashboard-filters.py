#!/usr/bin/env python3
"""Validate that every managed dashboard exposes and applies useful filters."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


EXPECTED_DASHBOARDS = 37
GRAFANA_BUILTINS = {"__all", "__interval", "__range", "__rate_interval"}
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
    "tqi-hosts-apm.json",
}
CONTAINER_DASHBOARDS = {
    "agent-fleet.json",
    "azure-overview.json",
    "capacity-forecast.json",
    "database-overview.json",
    "docker-overview.json",
    "frontend-observability.json",
    "logs.json",
    "web-vitals.json",
}
SYNTHETIC_DASHBOARDS = {
    "api-test-results.json",
    "browser-test-results.json",
    "incident-timeline.json",
    "release-comparison.json",
    "release-validation.json",
    "synthetic-monitoring.json",
}


def strings_from(value: object) -> list[str]:
    if isinstance(value, str):
        return [value]
    if isinstance(value, list):
        return [item for nested in value for item in strings_from(nested)]
    if isinstance(value, dict):
        return [item for nested in value.values() for item in strings_from(nested)]
    return []


def validate(path: Path) -> list[str]:
    errors: list[str] = []
    try:
        dashboard = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return [f"{path}: JSON inválido: {exc}"]

    variables = dashboard.get("templating", {}).get("list", [])
    if not variables:
        return [f"{path}: dashboard sem filtros"]

    declared = {variable.get("name") for variable in variables if variable.get("name")}
    searchable_text = "\n".join(strings_from(dashboard.get("panels", [])) + strings_from([v.get("query") for v in variables]))
    referenced_matches = re.findall(
        r"\$(?:\{([A-Za-z_][A-Za-z0-9_]*)(?::[^}]*)?\}|([A-Za-z_][A-Za-z0-9_]*))",
        searchable_text,
    )
    referenced = {left or right for left, right in referenced_matches}
    undeclared = referenced - declared - GRAFANA_BUILTINS
    if undeclared:
        errors.append(f"{path}: variáveis usadas mas não declaradas: {sorted(undeclared)}")

    unused = {
        name
        for name in declared
        if f"${name}" not in searchable_text and f"${{{name}" not in searchable_text
    }
    if unused:
        errors.append(f"{path}: filtros declarados mas não aplicados: {sorted(unused)}")

    for variable in variables:
        if variable.get("type") != "query":
            continue
        if variable.get("name") == "environment":
            if variable.get("multi") or variable.get("current", {}).get("value") != "production":
                errors.append(f"{path}: filtro environment deve iniciar em production e seleção única")
            continue
        if not variable.get("includeAll") or not variable.get("multi"):
            errors.append(f"{path}: filtro {variable.get('name')} não aceita seleção múltipla/All")
        if variable.get("current", {}).get("value") != "$__all":
            errors.append(f"{path}: filtro {variable.get('name')} não inicia em All")

    if path.name in APPLICATION_DASHBOARDS and "application" not in declared:
        errors.append(f"{path}: dashboard de aplicação sem filtro application")
    if path.name in APPLICATION_DASHBOARDS:
        route_variables = [variable for variable in variables if variable.get("name") == "route"]
        if path.name != "service-graph.json" and (not route_variables or any(variable.get("type") != "textbox" for variable in route_variables)):
            errors.append(f"{path}: rota deve ser filtro regex explícito, não uma lista de alta cardinalidade")
        if any(variable.get("current", {}).get("value") != ".*" for variable in route_variables):
            errors.append(f"{path}: rota não inicia com regex segura .* ")
        application_variables = [variable for variable in variables if variable.get("name") == "application"]
        if any(variable.get("regex") for variable in application_variables):
            errors.append(f"{path}: application remove namespace e pode colidir identidades")
        trace_queries = [
            target.get("query", "")
            for panel in dashboard.get("panels", [])
            for target in panel.get("targets", [])
            if target.get("queryType") == "traceql"
        ]
        if any("$route" in query for query in trace_queries):
            errors.append(f"{path}: TraceQL recebe expansão excessiva do top-200 de rotas")
    if path.name in CONTAINER_DASHBOARDS and "container" not in declared:
        errors.append(f"{path}: dashboard Docker/log sem filtro de aplicação/container")
    if path.name == "logs.json" and not {"log_source", "container_id", "stream", "search"}.issubset(declared):
        errors.append(f"{path}: dashboard de logs sem filtros operacionais completos")
    if path.name == "logs.json":
        log_sources = [variable for variable in variables if variable.get("name") == "log_source"]
        source_query = "\n".join(strings_from([variable.get("query") for variable in log_sources]))
        if not {"docker-container", "linux-system"}.issubset(set(re.findall(r"[A-Za-z0-9_-]+", source_query))):
            errors.append(f"{path}: fontes reais de logs não estão disponíveis no filtro")
    container_id_variables = [variable for variable in variables if variable.get("name") == "container_id"]
    if any(variable.get("allValue") for variable in container_id_variables):
        errors.append(f"{path}: All de container_id ignora o container selecionado")
    container_variables = [variable for variable in variables if variable.get("name") == "container"]
    if any(variable.get("allValue") != ".+" for variable in container_variables):
        errors.append(f"{path}: All de container deve excluir cgroups sem nome")
    if path.name in SYNTHETIC_DASHBOARDS and not {"probe_job", "target"}.issubset(declared):
        errors.append(f"{path}: dashboard sintético sem filtros de grupo e alvo")
    if path.name == "vmware-vm-performance.json" and not {"endpoint", "vm", "datastore"}.issubset(declared):
        errors.append(f"{path}: dashboard VMware sem endpoint, VM e datastore")

    forbidden = {
        'host_name="tqi-platform"': "host fixo",
        'job=~"tqi-platform/.+"': "aplicações consolidadas em job fixo",
        'job="blackbox-production"': "grupo sintético fixo",
        "demo_pipeline_requests_total": "métrica demo",
        "sentinel-demo": "fonte demo",
    }
    for pattern, reason in forbidden.items():
        if pattern in searchable_text:
            errors.append(f"{path}: {reason}: {pattern}")
    return errors


def main() -> int:
    root = Path(__file__).resolve().parents[1]
    dashboard_paths = sorted((root / "dashboards" / "managed").glob("*.json"))
    errors: list[str] = []
    if len(dashboard_paths) != EXPECTED_DASHBOARDS:
        errors.append(f"esperadas {EXPECTED_DASHBOARDS} dashboards, encontradas {len(dashboard_paths)}")
    for dashboard_path in dashboard_paths:
        errors.extend(validate(dashboard_path))
    if errors:
        print("Falha na validação dos filtros:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print(f"Filtros validados em {len(dashboard_paths)} dashboards.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
