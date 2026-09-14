package synthetics

import (
	"net/http"
	"net/netip"
	"testing"
	"time"
)

func TestValidateHTTPScenarioSpecRequiresExecutableAssertions(t *testing.T) {
	valid := map[string]any{
		"url": "https://portal.tqi.example/health", "method": "GET", "timeout": "10s",
		"assertions": []any{map[string]any{"type": "status_exact", "expected": 200}},
	}
	if err := ValidateHTTPScenarioSpec(valid); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	invalid := map[string]any{
		"url": "https://portal.tqi.example/health", "method": "GET", "timeout": "10s",
		"assertions": []any{map[string]any{"field": "status", "operator": "lessThan", "value": 400}},
	}
	if err := ValidateHTTPScenarioSpec(invalid); err == nil {
		t.Fatal("legacy illustrative assertion was accepted")
	}
}

func TestEvaluateAssertionsRejectsBusinessErrorWithHTTP200(t *testing.T) {
	maxLatency := int64(100)
	results := evaluateAssertions([]HTTPAssertion{
		{Type: "status_exact", Expected: float64(200)},
		{Type: "json_path_equals", Path: "$.ok", Expected: true},
		{Type: "header_equals", Name: "X-Contract-Version", Value: "2026-09"},
		{Type: "latency_max_ms", MaxMS: &maxLatency},
	}, 200, http.Header{"X-Contract-Version": []string{"2026-09"}}, []byte(`{"ok":false,"error":"dependency unavailable"}`), 25*time.Millisecond)
	if allPass(results) {
		t.Fatal("HTTP 200 with failed business contract passed")
	}
	if results[1].Status != "FAIL" {
		t.Fatalf("business assertion status = %s, want FAIL", results[1].Status)
	}
}

func TestExplicitTargetPolicyRequiresApprovedHostAndAddress(t *testing.T) {
	policy := newTargetPolicy([]AllowedTarget{{Host: "portal.tqi.example", Prefixes: []netip.Prefix{netip.MustParsePrefix("10.10.20.0/24")}}})
	if err := policy.authorizeHost("metadata.google.internal"); err == nil {
		t.Fatal("unapproved metadata host accepted")
	}
	if policy.allowedAddress("portal.tqi.example", netip.MustParseAddr("169.254.169.254")) {
		t.Fatal("metadata IP accepted outside configured site CIDR")
	}
	if !policy.allowedAddress("portal.tqi.example", netip.MustParseAddr("10.10.20.25")) {
		t.Fatal("approved private target rejected")
	}
}

func TestParseAllowedTargetsRejectsHostnameOnlyRule(t *testing.T) {
	if _, err := ParseAllowedTargets("demo-api"); err == nil {
		t.Fatal("hostname-only allowlist accepted")
	}
	if _, err := ParseAllowedTargets("demo-api@172.16.0.0/12"); err != nil {
		t.Fatalf("valid explicit target rejected: %v", err)
	}
}
