#!/usr/bin/env python3
"""Fail closed when a managed dashboard regresses NOC readability."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


EXPECTED_DASHBOARDS = 37
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
ONBOARDING_DASHBOARDS = {
    "api-test-results.json",
    "aws-overview.json",
    "browser-test-results.json",
    "continuous-profiling.json",
    "ecs-overview.json",
    "incident-timeline.json",
    "kubernetes-overview.json",
    "messaging-overview.json",
    "release-comparison.json",
    "release-validation.json",
    "synthetic-monitoring.json",
    "vmware-overview.json",
}


def target_text(panel: dict[str, object]) -> str:
    return "\n".join(
        str(target.get(field, ""))
        for target in panel.get("targets", [])
        for field in ("expr", "query", "labelSelector", "legendFormat")
    )


def validate(path: Path) -> list[str]:
    errors: list[str] = []
    dashboard = json.loads(path.read_text(encoding="utf-8"))
    if not str(dashboard.get("description", "")).strip():
        errors.append("dashboard sem description operacional")
    if "noc-ready" not in dashboard.get("tags", []):
        errors.append("dashboard sem tag noc-ready")

    variables = {
        item.get("name"): item
        for item in dashboard.get("templating", {}).get("list", [])
        if item.get("name")
    }
    environment = variables.get("environment")
    if environment and environment.get("current", {}).get("value") != "production":
        errors.append("filtro environment não inicia em production")
    container = variables.get("container")
    if container and container.get("allValue") != ".+":
        errors.append("container=All pode casar cgroups sem nome; esperado allValue '.+'");

    if path.name in APPLICATION_DASHBOARDS:
        application = variables.get("application", {})
        if application.get("regex"):
            errors.append("application remove namespace e pode colidir service.name/job")
        route = variables.get("route", {})
        if path.name != "service-graph.json" and (route.get("type") != "textbox" or route.get("current", {}).get("value") != ".*"):
            errors.append("rota de detalhe deve ser regex explícita com padrão .*; não lista top-200")

    for panel in dashboard.get("panels", []):
        panel_id = panel.get("id", "?")
        panel_type = panel.get("type")
        text = target_text(panel)
        prefix = f"painel {panel_id} ({panel.get('title', '')})"
        if not str(panel.get("description", "")).strip():
            errors.append(f"{prefix}: sem description")
        if "{{filename}}" in text or "{{container_id}}" in text:
            errors.append(f"{prefix}: legenda expõe caminho ou ID ilegível")

        if panel_type in {"stat", "timeseries"}:
            defaults = panel.get("fieldConfig", {}).get("defaults", {})
            for field in ("noValue", "unit", "decimals", "thresholds"):
                if field not in defaults:
                    errors.append(f"{prefix}: fieldConfig.defaults sem {field}")
        if panel_type == "stat":
            options = panel.get("options", {})
            if options.get("reduceOptions", {}).get("calcs") != ["lastNotNull"]:
                errors.append(f"{prefix}: redução não é lastNotNull")
            exprs = [str(target.get("expr", "")).strip() for target in panel.get("targets", [])]
            for expr in exprs:
                if re.match(r"^(sum|avg|max|min|count) by\s*\(", expr):
                    errors.append(f"{prefix}: stat retorna vetor agrupado")
                if expr.startswith(("topk(", "bottomk(")):
                    errors.append(f"{prefix}: stat recebe múltiplas séries topk/bottomk")
        elif panel_type == "timeseries":
            legend = panel.get("options", {}).get("legend", {})
            if legend.get("displayMode") != "table" or not legend.get("calcs"):
                errors.append(f"{prefix}: legenda não resume último/máximo")
            if re.search(r"sum by\s*\([^)]*http_route", text) and "topk(" not in text:
                errors.append(f"{prefix}: rotas sem limite topk")
        elif panel_type == "logs":
            options = panel.get("options", {})
            if not options.get("wrapLogMessage") or options.get("sortOrder") != "Descending":
                errors.append(f"{prefix}: UX de logs sem wrap/ordem recente")
            if any(int(target.get("maxLines", 0)) > 200 or not target.get("maxLines") for target in panel.get("targets", [])):
                errors.append(f"{prefix}: consulta de logs sem limite de 200 linhas")
            if path.name in APPLICATION_DASHBOARDS and 'job="linux-system"' in text:
                errors.append(f"{prefix}: logs de sistema misturados com aplicação")
        elif panel_type == "table":
            if panel.get("fieldConfig", {}).get("defaults", {}).get("noValue") != "Sem dados":
                errors.append(f"{prefix}: tabela sem noValue Sem dados")

    if path.name == "noc-overview.json":
        all_text = "\n".join(target_text(panel) for panel in dashboard.get("panels", []))
        if 'ALERTS{alertstate="firing"}' not in all_text or 'severity="critical"' not in all_text:
            errors.append("landing NOC não exibe alertas firing e críticos")
        if not any(panel.get("type") == "table" and "ALERTS" in target_text(panel) for panel in dashboard.get("panels", [])):
            errors.append("landing NOC sem fila tabular de alertas")
        noc_panels = {panel.get("id"): panel for panel in dashboard.get("panels", [])}
        hosts_missing = noc_panels.get(3, {}).get("fieldConfig", {}).get("defaults", {})
        expected_failure_steps = [
            {"color": "green", "value": None},
            {"color": "red", "value": 1},
        ]
        if hosts_missing.get("thresholds", {}).get("steps") != expected_failure_steps:
            errors.append("Hosts sem coleta deve ser verde em 0 e vermelho a partir de 1")
        http_5xx = noc_panels.get(6, {}).get("fieldConfig", {}).get("defaults", {})
        if http_5xx.get("unit") != "percent":
            errors.append("Taxa HTTP 5xx deve usar unidade percent")
    if path.name in {"apm.json", "application-overview.json"}:
        apm_panels = {panel.get("id"): panel for panel in dashboard.get("panels", [])}
        expected_units = {
            1: "short",
            2: "reqps",
            3: "s",
            5: "reqps",
            6: "percentunit",
        }
        for panel_id, expected_unit in expected_units.items():
            actual_unit = (
                apm_panels.get(panel_id, {})
                .get("fieldConfig", {})
                .get("defaults", {})
                .get("unit")
            )
            if actual_unit != expected_unit:
                errors.append(
                    f"painel APM {panel_id}: unidade {actual_unit!r}; esperado {expected_unit!r}"
                )
        route_volume = target_text(apm_panels.get(7, {}))
        if "topk(10" not in route_volume or "> 0" not in route_volume:
            errors.append("volume APM deve limitar a 10 rotas ativas e omitir séries zeradas")
    if path.name in ONBOARDING_DASHBOARDS:
        panels = dashboard.get("panels", [])
        if len(panels) > 2 or not any(panel.get("type") == "text" for panel in panels):
            errors.append("fonte ausente deve usar status compacto de onboarding")
        probe_panels = [panel for panel in panels if panel.get("title") == "Sonda real disponível"]
        for panel in probe_panels:
            defaults = panel.get("fieldConfig", {}).get("defaults", {})
            expected_status_steps = [
                {"color": "red", "value": None},
                {"color": "green", "value": 1},
            ]
            if defaults.get("thresholds", {}).get("steps") != expected_status_steps:
                errors.append("sonda deve ser vermelha em 0/ausência e verde somente a partir de 1")
            if defaults.get("noValue") != "Sem dados":
                errors.append("sonda sem amostra deve exibir Sem dados")
    return [f"{path.name}: {error}" for error in errors]


def main() -> int:
    root = Path(__file__).resolve().parents[1]
    paths = sorted((root / "dashboards" / "managed").glob("*.json"))
    errors: list[str] = []
    if len(paths) != EXPECTED_DASHBOARDS:
        errors.append(f"esperadas {EXPECTED_DASHBOARDS} dashboards, encontradas {len(paths)}")
    for path in paths:
        errors.extend(validate(path))
    if errors:
        print("Falha no gate de UX/NOC:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print(f"UX/NOC validada em {len(paths)} dashboards.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
