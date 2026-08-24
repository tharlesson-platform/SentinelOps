package workflows

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sentinelops/sentinelops/internal/domain"
)

func TestEvaluateMetricGateRequiresSamplesAndThreshold(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Fatalf("tenant header ausente: %q", r.Header.Get("X-Scope-OrgID"))
		}
		if r.URL.Query().Get("query") != "error_ratio" {
			t.Fatalf("unexpected query: %s", r.URL.Query().Get("query"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"0.02"]}]}}`))
	}))
	defer server.Close()
	a := Activities{HTTPClient: &http.Client{Timeout: time.Second}}
	spec := metricGateSpec{Name: "promql", Source: "prometheus", BaseURL: server.URL, Path: "/api/v1/query", QueryKey: "query", Prefix: "gate_promql"}

	pass := a.evaluateMetricGate(context.Background(), "tenant-a", map[string]string{"gate_promql": "error_ratio", "gate_promql_max": "0.05", "gate_promql_min_samples": "1"}, spec)
	if pass.Status != "PASS" {
		t.Fatalf("expected PASS, got %#v", pass)
	}
	fail := a.evaluateMetricGate(context.Background(), "tenant-a", map[string]string{"gate_promql": "error_ratio", "gate_promql_max": "0.01"}, spec)
	if fail.Status != "FAIL" {
		t.Fatalf("expected FAIL, got %#v", fail)
	}
	inconclusive := a.evaluateMetricGate(context.Background(), "tenant-a", map[string]string{"gate_promql": "error_ratio", "gate_promql_max": "1", "gate_promql_min_samples": "2"}, spec)
	if inconclusive.Status != "INCONCLUSIVE" {
		t.Fatalf("expected INCONCLUSIVE, got %#v", inconclusive)
	}
}

func TestTelemetryPolicyMissingFailsClosed(t *testing.T) {
	a := Activities{HTTPClient: &http.Client{Timeout: time.Second}}
	checks := a.evaluateTelemetryGates(context.Background(), "tenant-a", domain.Release{Labels: map[string]string{}})
	if len(checks) != 4 || validationResult(checks) != "INCONCLUSIVE" {
		t.Fatalf("missing policy must be inconclusive: %#v", checks)
	}
}

func TestTraceGateCountsMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Fatalf("tenant header ausente: %q", r.Header.Get("X-Scope-OrgID"))
		}
		_, _ = w.Write([]byte(`{"traces":[{"traceID":"1"}]}`))
	}))
	defer server.Close()
	a := Activities{HTTPClient: &http.Client{Timeout: time.Second}, TempoURL: server.URL}
	check := a.evaluateTraceGate(context.Background(), "tenant-a", map[string]string{"gate_traceql": `{ status = error }`, "gate_traceql_max_matches": "0"})
	if check.Status != "FAIL" {
		t.Fatalf("expected FAIL, got %#v", check)
	}
}

func TestValidationResultPrecedence(t *testing.T) {
	checks := []domain.ValidationCheck{{Required: true, Status: "INCONCLUSIVE"}, {Required: true, Status: "FAIL"}}
	if got := validationResult(checks); got != "FAIL" {
		t.Fatalf("expected FAIL, got %s", got)
	}
}
