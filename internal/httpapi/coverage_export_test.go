package httpapi

import (
	"encoding/json"
	"github.com/sentinelops/sentinelops/dashboards"
	"os"
	"testing"
	"time"
)

func TestExportCoveragePlan(t *testing.T) {
	if os.Getenv("SENTINEL_EXPORT_COVERAGE") == "" {
		t.Skip("operational artifact export only")
	}
	ds, _ := dashboards.Load()
	plans := []map[string]any{}
	for _, d := range ds {
		values := map[string]string{}
		resolved := map[string]string{}
		variableQueries := map[string]any{}
		for _, v := range d.Templating.List {
			values[v.Name] = v.Default()
			if v.Name == "metric" {
				values[v.Name] = "up"
			}
			if v.IncludeAll && v.AllValue == "" {
				resolved[v.Name] = "__RESOLVE_" + v.Name + "__"
				q, _, err := renderTemplate(v.Expression(), d, values, time.Hour, 15*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				variableQueries[v.Name] = map[string]string{"query": q, "regex": v.Regex, "source": v.Source.Type}
			}
		}
		for _, p := range d.Panels {
			targets := []map[string]any{}
			for _, target := range p.Targets {
				expr := target.Expr
				if expr == "" {
					expr = target.Query
				}
				q, filters, err := renderResolvedTemplate(expr, d, values, resolved, time.Hour, 15*time.Second)
				if err != nil {
					t.Fatal(d.UID, p.ID, err)
				}
				source := target.Source.Type
				if source == "" {
					source = p.Source.Type
				}
				targets = append(targets, map[string]any{"refId": target.RefID, "query": q, "source": source, "instant": target.Instant, "filters": filters})
			}
			plans = append(plans, map[string]any{"dashboard": d.UID, "panel": p.ID, "type": p.Type, "title": p.Title, "unit": p.FieldConfig.Defaults.Unit, "variables": values, "variableQueries": variableQueries, "targets": targets})
		}
	}
	data, _ := json.MarshalIndent(plans, "", "  ")
	if err := os.WriteFile("../../artifacts/coverage-2026-09-21/query-plan.json", data, 0600); err != nil {
		t.Fatal(err)
	}
}
