package assistant

import "testing"

func validRequest() Request {
	return Request{TenantID: "tenant-a", Question: "O que mudou?", Enabled: true, ProviderAvailable: true, BudgetUnits: 10, CostUnits: 1, Evidence: []Evidence{{ID: "ev-1", TenantID: "tenant-a", Source: "loki", Window: "2026-09-09T10:00:00Z/2026-09-09T10:05:00Z", Query: "{service=api}", FactKey: "deployment", Value: "v2"}}}
}

func TestEvaluateReturnsCitationsOnlyForProvenancedTenantEvidence(t *testing.T) {
	decision := Evaluate(validRequest())
	if decision.State != "ANSWER" || decision.Reason != "evidence_available" || len(decision.Citations) != 1 || len(decision.ToolCalls) != 0 {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestEvaluateAbstainsForTenantMismatchContradictionBudgetAndProvider(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Request)
		reason string
	}{
		{"tenant", func(r *Request) { r.Evidence[0].TenantID = "tenant-b" }, "tenant_mismatch"},
		{"contradiction", func(r *Request) {
			r.Evidence = append(r.Evidence, Evidence{ID: "ev-2", TenantID: "tenant-a", Source: "prometheus", Window: "2026-09-09T10:00:00Z/2026-09-09T10:05:00Z", Query: "up", FactKey: "deployment", Value: "v3"})
		}, "evidence_contradictory"},
		{"budget", func(r *Request) { r.CostUnits = 11 }, "budget_exceeded"},
		{"budget warning", func(r *Request) { r.BudgetWarningUnits = 1 }, "budget_warning"},
		{"provider", func(r *Request) { r.ProviderAvailable = false }, "provider_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := validRequest()
			tc.mutate(&request)
			decision := Evaluate(request)
			if decision.State != "ABSTAIN" || decision.Reason != tc.reason || len(decision.ToolCalls) != 0 {
				t.Fatalf("unexpected decision: %#v", decision)
			}
		})
	}
}
