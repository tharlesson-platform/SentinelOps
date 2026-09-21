package httpapi

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sentinelops/sentinelops/dashboards"
)

func TestExplorerLiteralVariablesAndDefaults(t *testing.T) {
	var d dashboards.Dashboard
	if err := json.Unmarshal([]byte(`{"templating":{"list":[{"name":"host","type":"query","includeAll":true,"allValue":".+","current":{"value":"$__all"}},{"name":"environment","type":"query","includeAll":true,"allValue":".*","current":{"value":"production"}},{"name":"search","type":"textbox","current":{"value":".*"}},{"name":"container_id","type":"query","includeAll":true,"current":{"value":"$__all"}}]}}`), &d); err != nil {
		t.Fatal(err)
	}
	template := `up{host=~"$host",environment=~"${environment:regex}"}`
	query, _, err := renderTemplate(template, d, nil, time.Hour, 15*time.Second)
	if err != nil || query != `up{host=~".+",environment=~"production"}` {
		t.Fatalf("default changed: %s %v", query, err)
	}
	for _, value := range []string{`a|b`, `a.*`, `x"} or up{job="y`, `$__range`, `c:\\path`, `prod[1]`} {
		query, _, err = renderTemplate(`"$host"`, d, map[string]string{"host": value}, time.Hour, 15*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		pattern, e := strconv.Unquote(query)
		if e != nil {
			t.Fatal(e)
		}
		re, e := regexp.Compile("^(?:" + pattern + ")$")
		if e != nil {
			t.Fatal(e)
		}
		if !re.MatchString(value) || re.MatchString(value+"other") {
			t.Fatalf("not literal: %q -> %q", value, query)
		}
	}
	if _, _, err = renderTemplate(`"$container_id"`, d, nil, time.Hour, time.Minute); err == nil {
		t.Fatal("unresolved All widened query")
	}
	query, _, err = renderResolvedTemplate(`"$container_id"`, d, nil, map[string]string{"container_id": "(id1|id2)"}, time.Hour, time.Minute)
	if err != nil || query != `"(id1|id2)"` {
		t.Fatalf("resolved union: %s %v", query, err)
	}
	if _, _, err = renderTemplate(`$missing`, d, nil, time.Hour, time.Minute); err == nil {
		t.Fatal("unknown macro accepted")
	}
	if _, _, err = renderTemplate(`up`, d, map[string]string{"unknown": "x"}, time.Hour, time.Minute); err == nil {
		t.Fatal("unknown filter accepted")
	}
}

func TestAllProvisionedQueriesRenderWithExplicitVariables(t *testing.T) {
	ds, err := dashboards.Load()
	if err != nil {
		t.Fatal(err)
	}
	panels, targets := 0, 0
	for _, d := range ds {
		values := map[string]string{}
		for _, v := range d.Templating.List {
			values[v.Name] = "literal-value"
		}
		for _, p := range d.Panels {
			panels++
			for _, target := range p.Targets {
				targets++
				expr := target.Expr
				if expr == "" {
					expr = target.Query
				}
				if _, _, err := renderTemplate(expr, d, values, time.Hour, 15*time.Second); err != nil {
					t.Errorf("%s/%d: %v", d.UID, p.ID, err)
				}
			}
		}
	}
	if len(ds) != 37 || panels != 188 || targets != 217 {
		t.Fatalf("catalog changed: %d/%d/%d", len(ds), panels, targets)
	}
}
func TestExactMetricSelectorDoesNotPermitExpressionInjection(t *testing.T) {
	if _, err := exactMetricSelector(`up or vector(1)`, nil); err == nil {
		t.Fatal("expression accepted")
	}
	if _, err := exactMetricSelector("up", map[string]string{"__name__": "other"}); err == nil {
		t.Fatal("metric override accepted")
	}
	q, err := exactMetricSelector("up", map[string]string{"job": `x"} or up{job="y`})
	if err != nil || !strings.Contains(q, `job="x\"} or up{job=\"y"`) {
		t.Fatalf("unsafe selector %s %v", q, err)
	}
}

func TestTraceCatalogUsesCanonicalServiceAndHost(t *testing.T) {
	ds, err := dashboards.Load()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, d := range ds {
		application := false
		for _, v := range d.Templating.List {
			if v.Name == "trace_service" || v.Name == "trace_host" {
				t.Fatal("cartesian association variable remains")
			}
			if v.Name == "application" {
				application = true
				if !strings.Contains(v.Expression(), "service_name)") || !strings.Contains(v.Expression(), `host_name=~"$host"`) {
					t.Fatalf("invalid service discovery %s", v.Expression())
				}
			}
		}
		if !application {
			continue
		}
		for _, p := range d.Panels {
			for _, target := range p.Targets {
				expr := target.Expr
				if expr == "" {
					expr = target.Query
				}
				if !strings.Contains(expr, "$application") {
					continue
				}
				if strings.Contains(expr, "target_info") || strings.Contains(expr, "group_left") || strings.Contains(expr, `job=~`) || strings.Contains(expr, "service.namespace") {
					t.Fatalf("heuristic identity remains: %s", expr)
				}
				for _, scenario := range []struct{ service, host, wantService, wantHost string }{
					{"worklog", "tqi-platform", "worklog", "tqi-platform"},
					{"worklog", "easy-vm", "worklog", "easy-vm"},
					{"worklog", "__all__", "worklog", ".*"},
					{"__all__", "tqi-platform", ".+", "tqi-platform"},
					{"tqi-platform/worklog", "tqi-platform", "tqi-platform/worklog", "tqi-platform"},
				} {
					q, _, err := renderTemplate(expr, d, map[string]string{"application": scenario.service, "host": scenario.host}, time.Hour, time.Minute)
					if err != nil {
						t.Fatal(err)
					}
					serviceMatcher, hostMatcher := `service_name=~"`+scenario.wantService+`"`, `host_name=~"`+scenario.wantHost+`"`
					if target.Query != "" {
						serviceMatcher, hostMatcher = `resource.service.name =~ "`+scenario.wantService+`"`, `resource.host.name =~ "`+scenario.wantHost+`"`
					}
					if !strings.Contains(q, serviceMatcher) || !strings.Contains(q, hostMatcher) {
						t.Fatalf("%s/%d inconsistent selection: %s", d.UID, p.ID, q)
					}
					if target.Query == "" && (!strings.Contains(q, `host_name!~".*;.*"`) || !strings.Contains(q, `host_name!=""`)) {
						t.Fatalf("invalid historical identities accepted: %s", q)
					}
				}
				if target.Query != "" {
					count++
				}
			}
		}
	}
	if count != 10 {
		t.Fatalf("expected ten canonical trace panels, got %d", count)
	}
}

func TestLogsCatalogNativeTemplatesKeepDisjointTargetsAndDiscovery(t *testing.T) {
	ds, err := dashboards.Load()
	if err != nil {
		t.Fatal(err)
	}
	var logs dashboards.Dashboard
	for _, d := range ds {
		if d.UID == "sentinel-logs" {
			logs = d
		}
	}
	if len(logs.Panels) != 5 {
		t.Fatalf("logs panels: %d", len(logs.Panels))
	}
	id := strings.Repeat("a", 64)
	resolved := map[string]string{"container_id": "(" + id + ")"}
	values := map[string]string{"host": "host-a", "environment": "production", "stream": "stdout"}
	for _, p := range logs.Panels {
		if len(p.Targets) != 3 {
			t.Fatalf("panel %d must stay below native four-target limit: %d", p.ID, len(p.Targets))
		}
		for _, target := range p.Targets {
			query, applied, err := renderResolvedTemplate(target.Expr, logs, values, resolved, 30*time.Minute, 30*time.Second)
			if err != nil {
				t.Fatalf("panel %d/%s: %v", p.ID, target.RefID, err)
			}
			if target.LegendFormat == "" || !strings.Contains(p.Description, target.RefID+" = ") {
				t.Fatalf("panel %d/%s needs a readable legend and description", p.ID, target.RefID)
			}
			if applied["host"] != "host-a" || applied["environment"] != "production" {
				t.Fatalf("lost host/environment scope: %s", query)
			}
			switch target.RefID {
			case "A":
				if !strings.Contains(query, `container_id!="",container_id=~"(`+id+`)"`) || strings.Contains(query, "filename") {
					t.Fatalf("ID query depends on legacy filename: %s", query)
				}
			case "B":
				if !strings.Contains(query, `job!="docker-container"`) || strings.Contains(query, "container_id") || strings.Contains(query, "stream=") {
					t.Fatalf("system query gained Docker scope: %s", query)
				}
			case "C":
				if !strings.Contains(query, `container_id="",filename=~".*/((`+id+`))/.*"`) {
					t.Fatalf("legacy fallback is not disjoint/scoped: %s", query)
				}
			default:
				t.Fatalf("unexpected target %s", target.RefID)
			}
		}
	}
	streamFound := false
	for _, v := range logs.Templating.List {
		if v.Name != "stream" {
			continue
		}
		query, _, err := renderTemplate(v.Expression(), logs, values, 30*time.Minute, 30*time.Second)
		if err != nil {
			t.Fatalf("stream discovery must render with unresolved container All: %v", err)
		}
		if query != `label_values({job="docker-container",deployment_environment=~"production",host_name=~"host-a"}, stream)` {
			t.Fatalf("unexpected stream discovery: %s", query)
		}
		streamFound = true
	}
	if !streamFound {
		t.Fatal("stream variable missing")
	}
	// Validate the same rendered identity contract in every related dashboard,
	// including metric-count panels that used to have an incompatible logs type.
	relatedDashboards := map[string]bool{}
	relatedPanels := 0
	for _, d := range ds {
		if d.UID == logs.UID {
			continue
		}
		for _, p := range d.Panels {
			legacy := false
			for _, target := range p.Targets {
				legacy = legacy || strings.Contains(target.Expr, "filename")
			}
			if !legacy {
				continue
			}
			relatedPanels++
			relatedDashboards[d.UID] = true
			if len(p.Targets) != 2 || p.Targets[0].RefID != "A" || p.Targets[1].RefID != "B" {
				t.Fatalf("%s/%d must have two distinct targets", d.UID, p.ID)
			}
			for _, target := range p.Targets {
				query, applied, err := renderResolvedTemplate(target.Expr, d,
					map[string]string{"host": "host-a", "environment": "production"},
					resolved, 30*time.Minute, 30*time.Second)
				if err != nil || applied["host"] != "host-a" || applied["environment"] != "production" {
					t.Fatalf("%s/%d/%s scope/render failed: %s %v", d.UID, p.ID, target.RefID, query, err)
				}
				if target.RefID == "A" {
					if !strings.Contains(query, `container_id!="",container_id=~"(`+id+`)"`) || strings.Contains(query, "filename") {
						t.Fatalf("%s/%d ID query depends on filename: %s", d.UID, p.ID, query)
					}
				} else if !strings.Contains(query, `container_id="",filename=~".*/((`+id+`))/.*"`) {
					t.Fatalf("%s/%d legacy scope is not disjoint: %s", d.UID, p.ID, query)
				}
				metricCount := strings.HasPrefix(query, "sum(count_over_time(")
				if (metricCount && p.Type != "stat") || (!metricCount && p.Type != "logs") {
					t.Fatalf("%s/%d type %s incompatible with query %s", d.UID, p.ID, p.Type, query)
				}
				if target.LegendFormat == "" || !strings.Contains(p.Description, target.RefID+" = ") {
					t.Fatalf("%s/%d/%s missing readable legend/description", d.UID, p.ID, target.RefID)
				}
			}
		}
	}
	if relatedPanels != 19 || len(relatedDashboards) != 16 {
		t.Fatalf("related catalog coverage changed: %d panels in %d dashboards", relatedPanels, len(relatedDashboards))
	}
}

func TestProvisionedVariableRegexUsesCaptureAndDoesNotInventIDs(t *testing.T) {
	def := dashboards.Variable{Regex: `/docker-([a-f0-9]{64})\.scope$/`}
	id := strings.Repeat("a", 64)
	got, err := filterVariableValues(def, []string{"invalid", "/system.slice/docker-" + id + ".scope", "/system.slice/docker-" + id + ".scope"})
	if err != nil || len(got) != 1 || got[0] != id {
		t.Fatalf("capture %v %v", got, err)
	}
}
