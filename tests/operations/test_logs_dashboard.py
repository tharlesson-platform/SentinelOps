"""Offline label-selection contract; does not claim to execute a Loki engine."""

import copy
import importlib.util
import json
from pathlib import Path
import re
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
RELATED_PANELS = {
    "alerts.json": [4], "apm.json": [4, 9], "application-overview.json": [4, 9],
    "azure-overview.json": [4], "database-overview.json": [4],
    "distributed-tracing.json": [4], "errors.json": [4],
    "frontend-observability.json": [4], "latency.json": [4],
    "sentinelops-self-monitoring.json": [4], "service-graph.json": [4],
    "service-health.json": [4], "slo-error-budget.json": [4],
    "throughput.json": [4], "tqi-hosts-apm.json": [6, 15], "web-vitals.json": [4],
}
COUNT_PANELS = {
    ("apm.json", 4): "5m", ("application-overview.json", 4): "5m",
    ("distributed-tracing.json", 4): "5m", ("latency.json", 4): "5m",
    ("service-graph.json", 4): "5m", ("service-health.json", 4): "5m",
    ("slo-error-budget.json", 4): "5m", ("throughput.json", 4): "5m",
    ("tqi-hosts-apm.json", 6): "15m",
}
spec = importlib.util.spec_from_file_location(
    "dashboard_filters", ROOT / "scripts/apply-dashboard-filters.py"
)
filters = importlib.util.module_from_spec(spec)
spec.loader.exec_module(filters)


class LogsDashboardTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.dashboard = json.loads((ROOT / "dashboards/managed/logs.json").read_text())
        cls.panels = {p["id"]: p for p in cls.dashboard["panels"]}
        cls.variables = {v["name"]: v for v in cls.dashboard["templating"]["list"]}

    def query(self, expression, **overrides):
        values = dict(log_source="docker-container", environment="production",
                      host="host-a", container_id="a" * 64, stream="stdout",
                      search="wanted", __range="1800s", __interval="30s")
        values.update(overrides)
        return re.sub(r"\$\{(\w+):regex\}|\$(\w+)",
                      lambda m: values[m[1] or m[2]], expression)

    def matches(self, expression, labels, line="wanted ERROR", **overrides):
        query = self.query(expression, **overrides)
        selector = re.search(r"\{([^{}]*)\}", query)[1]
        # Strictly accept the subset generated here. Missing labels have the
        # empty value per Loki/Prometheus matcher semantics; regexes are anchored.
        matchers = re.findall(r'(\w+)(=~|!~|!=|=)("(?:\\.|[^"\\])*")', selector)
        self.assertEqual(",".join("".join(m) for m in matchers), selector)
        for label, operator, encoded in matchers:
            pattern, value = json.loads(encoded), labels.get(label, "")
            matched = bool(re.fullmatch(pattern, value)) if "~" in operator else value == pattern
            if matched == operator.startswith("!"):
                return False
        return all(re.search(json.loads(encoded), line) is not None
                   for encoded in re.findall(r'\|~\s*("(?:\\.|[^"\\])*")', query))

    def hits(self, panel, labels, **overrides):
        return [t["refId"] for t in panel["targets"]
                if self.matches(t["expr"], labels, **overrides)]

    def base(self, **labels):
        return dict(job="docker-container", deployment_environment="production",
                    host_name="host-a", stream="stdout", **labels)

    def test_new_legacy_and_dual_labeled_events_are_counted_once(self):
        container_id = "a" * 64
        filename = f"/var/lib/docker/containers/{container_id}/container.log"
        rows = [(self.base(container_id=container_id), ["A"]),
                (self.base(filename=filename), ["C"]),
                (self.base(container_id=container_id, filename=filename), ["A"]),
                (self.base(container_id="", filename=filename), ["C"])]
        for panel in self.panels.values():
            with self.subTest(panel=panel["id"]):
                for labels, expected in rows:
                    self.assertEqual(self.hits(panel, labels), expected)
                self.assertEqual(sum(len(self.hits(panel, row)) for row, _ in rows), len(rows))

    def test_container_identity_wins_over_filename_and_rejects_other_scopes(self):
        good = self.base(container_id="a" * 64)
        legacy = self.base(filename=f'/containers/{"a" * 64}/json.log')
        for panel in self.panels.values():
            for row in (good, legacy):
                for key, value in (("host_name", "host-b"), ("deployment_environment", "staging"),
                                   ("stream", "stderr"), ("job", "linux-system")):
                    self.assertEqual(self.hits(panel, dict(row, **{key: value})), [])
                self.assertEqual(self.hits(panel, row, line="unrelated ERROR"), [])
                self.assertEqual(self.hits(panel, row, container_id="b" * 64), [])
            self.assertEqual(self.hits(panel, dict(legacy, container_id="b" * 64)), [])

    def test_linux_system_remains_separate_and_ignores_docker_only_filters(self):
        row = dict(self.base(), job="linux-system")
        row.pop("stream")
        for panel in self.panels.values():
            self.assertEqual(self.hits(panel, row), [])
            for source in ("linux-system", ".+"):
                self.assertEqual(self.hits(panel, row, log_source=source,
                                           container_id="b" * 64, stream="stderr"), ["B"])
                self.assertEqual(self.hits(panel, dict(row, host_name="host-b"), log_source=source), [])
                # A system stream cannot leak into Docker even with both labels.
                self.assertEqual(self.hits(panel, dict(row, container_id="a" * 64,
                                    filename=f'/containers/{"a" * 64}/json.log', stream="stdout"),
                                    log_source=source), ["B"])

    def test_discovery_supports_both_contracts_without_changing_data_scope(self):
        stream = self.variables["stream"]
        self.assertIn("host/ambiente", stream["label"])
        expression = stream["query"]["query"]
        self.assertTrue(expression.startswith("label_values("))
        self.assertTrue(expression.endswith(", stream)"))
        self.assertNotIn("filename", expression)
        self.assertNotIn("container_id", expression)
        for row in (self.base(container_id="a" * 64),
                    self.base(filename=f'/containers/{"a" * 64}/json.log')):
            self.assertTrue(self.matches(expression, row))
            self.assertFalse(self.matches(expression, dict(row, host_name="host-b")))
            self.assertFalse(self.matches(expression, dict(row, deployment_environment="staging")))
        self.assertNotIn("allValue", self.variables["container_id"])
        self.assertEqual(self.variables["log_source"]["current"]["value"], "docker-container")
        self.assertEqual(self.dashboard["time"], {"from": "now-30m", "to": "now"})

    def test_period_and_query_results_remain_explicit(self):
        for panel_id, panel in self.panels.items():
            self.assertEqual([t["refId"] for t in panel["targets"]], ["A", "B", "C"])
            self.assertIn("sem total consolidado", panel["description"])
            for target in panel["targets"]:
                self.assertTrue(target["legendFormat"])
                self.assertNotIn(" or ", target["expr"])
                if panel_id == 4:
                    self.assertTrue(target["expr"].startswith("{"))
                    self.assertEqual(target["maxLines"], 200)
                else:
                    self.assertIn("count_over_time(", target["expr"])
                    self.assertIn("[$__range]" if panel_id in (1, 5) else "[$__interval]", target["expr"])
            if panel_id in (1, 5):
                self.assertEqual(panel["options"]["textMode"], "value_and_name")

    def test_regeneration_preserves_reviewed_dashboard(self):
        for filename in ["logs.json", *RELATED_PANELS]:
            with self.subTest(dashboard=filename), tempfile.TemporaryDirectory() as directory:
                dashboard = json.loads((ROOT / "dashboards/managed" / filename).read_text())
                path = Path(directory) / filename
                path.write_text(json.dumps(copy.deepcopy(dashboard)))
                filters.apply_filters(path)
                self.assertEqual(json.loads(path.read_text()), dashboard)

    def test_all_related_docker_queries_keep_identity_and_scope(self):
        self.assertEqual(sum(map(len, RELATED_PANELS.values())), 19)
        for filename, ids in RELATED_PANELS.items():
            dashboard = json.loads((ROOT / "dashboards/managed" / filename).read_text())
            for panel in dashboard["panels"]:
                if panel["id"] not in ids:
                    continue
                with self.subTest(dashboard=filename, panel=panel["id"]):
                    self.assertEqual([t["refId"] for t in panel["targets"]], ["A", "B"])
                    self.assertIn("sem total consolidado", panel["description"])
                    primary = self.base(container_id="a" * 64)
                    legacy = self.base(filename=f'/containers/{"a" * 64}/json.log')
                    self.assertEqual(self.hits(panel, primary), ["A"])
                    self.assertEqual(self.hits(panel, legacy), ["B"])
                    self.assertEqual(self.hits(panel, dict(legacy, container_id="a" * 64)), ["A"])
                    self.assertEqual(self.hits(panel, dict(legacy, container_id="b" * 64)), [])
                    for row in (primary, legacy):
                        for key, value in (("host_name", "host-b"), ("deployment_environment", "staging"),
                                           ("job", "linux-system")):
                            self.assertEqual(self.hits(panel, dict(row, **{key: value})), [])
                        self.assertEqual(self.hits(panel, row, container_id="b" * 64), [])
                    error_panel = filename in {"alerts.json", "errors.json"} or (filename, panel["id"]) == ("tqi-hosts-apm.json", 6)
                    self.assertEqual(self.hits(panel, primary, line="wanted healthy"), [] if error_panel else ["A"])

    def test_related_panel_types_match_logql_result_contract(self):
        for filename, ids in RELATED_PANELS.items():
            dashboard = json.loads((ROOT / "dashboards/managed" / filename).read_text())
            for panel in dashboard["panels"]:
                if panel["id"] not in ids:
                    continue
                with self.subTest(dashboard=filename, panel=panel["id"]):
                    period = COUNT_PANELS.get((filename, panel["id"]))
                    self.assertEqual(panel["type"], "stat" if period else "logs")
                    for target in panel["targets"]:
                        self.assertIn(target["refId"] + " = ", panel["description"])
                        self.assertEqual(target["legendFormat"], "Docker por ID" if target["refId"] == "A" else "Docker legado")
                        if period:
                            self.assertTrue(target["expr"].startswith("sum(count_over_time("))
                            self.assertTrue(target["expr"].endswith(f"[{period}]))"))
                            self.assertNotIn("maxLines", target)
                        else:
                            self.assertTrue(target["expr"].startswith("{"))
                            self.assertEqual(target["maxLines"], 200)
                    if period:
                        self.assertTrue(panel["title"].startswith("Eventos "))
                        self.assertEqual(panel["options"]["reduceOptions"]["calcs"], ["lastNotNull"])
                        self.assertEqual(panel["options"]["textMode"], "value_and_name")
                        self.assertEqual(panel["fieldConfig"]["defaults"]["unit"], "short")

    def test_catalog_has_no_unpaired_filename_selectors_or_excessive_targets(self):
        dashboards = list((ROOT / "dashboards/managed").glob("*.json"))
        panels = [panel for path in dashboards for panel in json.loads(path.read_text())["panels"]]
        self.assertEqual((len(dashboards), len(panels), sum(len(p.get("targets", [])) for p in panels)), (37, 188, 217))
        affected = set()
        for path in dashboards:
            for panel in json.loads(path.read_text())["panels"]:
                targets = panel.get("targets", [])
                self.assertLessEqual(len(targets), 4)
                self.assertEqual(len({t["refId"] for t in targets}), len(targets))
                for target in targets:
                    if "filename" in target.get("expr", ""):
                        affected.add(path.name)
                        self.assertIn('container_id=""', target["expr"])
                        self.assertTrue(any('container_id!=""' in t.get("expr", "") and "filename" not in t["expr"] for t in targets))
        self.assertEqual(affected, {"logs.json", *RELATED_PANELS})


if __name__ == "__main__":
    unittest.main()
