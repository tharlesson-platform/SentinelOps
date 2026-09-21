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
	if len(ds) != 37 || panels != 188 || targets != 193 {
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
func TestProvisionedVariableRegexUsesCaptureAndDoesNotInventIDs(t *testing.T) {
	def := dashboards.Variable{Regex: `/docker-([a-f0-9]{64})\.scope$/`}
	id := strings.Repeat("a", 64)
	got, err := filterVariableValues(def, []string{"invalid", "/system.slice/docker-" + id + ".scope", "/system.slice/docker-" + id + ".scope"})
	if err != nil || len(got) != 1 || got[0] != id {
		t.Fatalf("capture %v %v", got, err)
	}
}
