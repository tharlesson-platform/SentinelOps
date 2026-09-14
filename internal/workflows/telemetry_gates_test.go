package workflows

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sentinelops/sentinelops/internal/domain"
)

func metricPolicy(query string) map[string]string {
	return map[string]string{
		"gate_promql":             query,
		"gate_promql_max":         "0.05",
		"gate_promql_min_samples": "2",
		"gate_promql_window":      "5m",
		"gate_promql_max_age":     "30s",
		"gate_promql_step":        "30s",
	}
}

func TestEvaluateMetricGateRequiresTemporalSamplesFreshnessAndThreshold(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Fatalf("tenant header ausente: %q", r.Header.Get("X-Scope-OrgID"))
		}
		if r.URL.Path != "/api/v1/query_range" || r.URL.Query().Get("query") != "error_ratio" || r.URL.Query().Get("step") != "30s" {
			t.Fatalf("query range inesperada: %s", r.URL.String())
		}
		now := time.Now().UTC().Unix()
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[%d,"0.02"],[%d,"0.03"]] }]}}`, now-10, now)
	}))
	defer server.Close()
	a := Activities{HTTPClient: &http.Client{Timeout: time.Second}}
	spec := metricGateSpec{Name: "promql", Source: "prometheus", BaseURL: server.URL, Path: "/api/v1/query_range", Prefix: "gate_promql"}

	pass := a.evaluateMetricGate(context.Background(), "tenant-a", metricPolicy("error_ratio"), spec)
	if pass.Status != "PASS" {
		t.Fatalf("expected PASS, got %#v", pass)
	}
	failing := metricPolicy("error_ratio")
	failing["gate_promql_max"] = "0.01"
	if got := a.evaluateMetricGate(context.Background(), "tenant-a", failing, spec); got.Status != "FAIL" {
		t.Fatalf("expected FAIL, got %#v", got)
	}
	insufficient := metricPolicy("error_ratio")
	insufficient["gate_promql_min_samples"] = "3"
	if got := a.evaluateMetricGate(context.Background(), "tenant-a", insufficient, spec); got.Status != "INCONCLUSIVE" {
		t.Fatalf("expected INCONCLUSIVE for sample count, got %#v", got)
	}
}

func TestEvaluateMetricGateRejectsStaleWarningsAndNonFiniteEvidence(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "stale", body: `{"status":"success","data":{"resultType":"matrix","result":[{"values":[[1,"0.01"],[2,"0.02"]]}]}}`},
		{name: "warning", body: `{"status":"success","warnings":["partial response"],"data":{"resultType":"matrix","result":[{"values":[[1,"0.01"],[2,"0.02"]]}]}}`},
		{name: "non finite", body: `{"status":"success","data":{"resultType":"matrix","result":[{"values":[[1,"NaN"],[2,"0.02"]]}]}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			a := Activities{HTTPClient: &http.Client{Timeout: time.Second}}
			spec := metricGateSpec{Name: "promql", Source: "prometheus", BaseURL: server.URL, Path: "/api/v1/query_range", Prefix: "gate_promql"}
			if got := a.evaluateMetricGate(context.Background(), "tenant-a", metricPolicy("error_ratio"), spec); got.Status != "INCONCLUSIVE" {
				t.Fatalf("expected INCONCLUSIVE, got %#v", got)
			}
		})
	}
}

func TestTelemetryPolicyMissingFailsClosed(t *testing.T) {
	a := Activities{HTTPClient: &http.Client{Timeout: time.Second}}
	checks := a.evaluateTelemetryGates(context.Background(), "tenant-a", domain.Release{Labels: map[string]string{}})
	if len(checks) != 4 || validationResult(checks) != "INCONCLUSIVE" {
		t.Fatalf("missing policy must be inconclusive: %#v", checks)
	}
}

func TestTraceGateRequiresPositiveCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Fatalf("tenant header ausente: %q", r.Header.Get("X-Scope-OrgID"))
		}
		switch r.URL.Query().Get("q") {
		case "{ status = error }":
			_, _ = w.Write([]byte(`{"traces":[]}`))
		case "{ resource.service.name = \"api\" }":
			_, _ = w.Write([]byte(`{"traces":[{"traceID":"1"}]}`))
		default:
			t.Fatalf("query TraceQL inesperada: %q", r.URL.Query().Get("q"))
		}
	}))
	defer server.Close()
	a := Activities{HTTPClient: &http.Client{Timeout: time.Second}, TempoURL: server.URL}
	labels := map[string]string{
		"gate_traceql":                      "{ status = error }",
		"gate_traceql_max_matches":          "0",
		"gate_traceql_coverage_query":       "{ resource.service.name = \"api\" }",
		"gate_traceql_min_coverage_matches": "1",
		"gate_traceql_window":               "5m",
	}
	if got := a.evaluateTraceGate(context.Background(), "tenant-a", labels); got.Status != "PASS" {
		t.Fatalf("expected PASS with coverage, got %#v", got)
	}
	labels["gate_traceql_min_coverage_matches"] = "2"
	if got := a.evaluateTraceGate(context.Background(), "tenant-a", labels); got.Status != "INCONCLUSIVE" {
		t.Fatalf("expected INCONCLUSIVE without coverage, got %#v", got)
	}
}

func TestValidationResultPrecedence(t *testing.T) {
	checks := []domain.ValidationCheck{{Required: true, Status: "INCONCLUSIVE"}, {Required: true, Status: "FAIL"}}
	if got := validationResult(checks); got != "FAIL" {
		t.Fatalf("expected FAIL, got %s", got)
	}
}
