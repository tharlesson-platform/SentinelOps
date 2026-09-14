// Package assistant provides the deterministic policy boundary that must run
// before a future model provider. It never invokes tools or a model itself.
package assistant

import (
	"strings"
)

type Evidence struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	Source   string `json:"source"`
	Window   string `json:"window"`
	Query    string `json:"query"`
	FactKey  string `json:"factKey,omitempty"`
	Value    string `json:"value,omitempty"`
}

type Request struct {
	TenantID           string     `json:"tenantId"`
	Question           string     `json:"question"`
	Enabled            bool       `json:"enabled"`
	ProviderAvailable  bool       `json:"providerAvailable"`
	CostUnits          int        `json:"costUnits"`
	BudgetUnits        int        `json:"budgetUnits"`
	BudgetWarningUnits int        `json:"budgetWarningUnits"`
	Evidence           []Evidence `json:"evidence"`
}

type Citation struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Window string `json:"window"`
	Query  string `json:"query"`
}

type Decision struct {
	State     string     `json:"state"`
	Reason    string     `json:"reason"`
	Citations []Citation `json:"citations"`
	ToolCalls []string   `json:"toolCalls"`
}

// Evaluate checks whether a provider would be allowed to receive an
// investigation request. An ANSWER decision is not a causal conclusion: it
// only proves that evidence has enough provenance to be summarized later.
func Evaluate(request Request) Decision {
	decision := Decision{State: "ABSTAIN", ToolCalls: []string{}}
	if !request.Enabled {
		decision.Reason = "assistant_disabled"
		return decision
	}
	if !request.ProviderAvailable {
		decision.Reason = "provider_unavailable"
		return decision
	}
	if strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.Question) == "" {
		decision.Reason = "invalid_request"
		return decision
	}
	if request.BudgetUnits < 0 || request.CostUnits < 0 || request.CostUnits > request.BudgetUnits {
		decision.Reason = "budget_exceeded"
		return decision
	}
	// A configured warning threshold is a fail-closed early warning: a future
	// provider cannot consume the final budget units while alerting is pending.
	if request.BudgetWarningUnits > 0 && (request.BudgetWarningUnits > request.BudgetUnits || request.CostUnits >= request.BudgetWarningUnits) {
		decision.Reason = "budget_warning"
		return decision
	}
	if len(request.Evidence) == 0 {
		decision.Reason = "evidence_missing"
		return decision
	}
	values := map[string]string{}
	citations := make([]Citation, 0, len(request.Evidence))
	for _, item := range request.Evidence {
		if item.TenantID != request.TenantID {
			decision.Reason = "tenant_mismatch"
			return decision
		}
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Source) == "" || strings.TrimSpace(item.Window) == "" || strings.TrimSpace(item.Query) == "" {
			decision.Reason = "evidence_provenance_missing"
			return decision
		}
		key, value := strings.TrimSpace(item.FactKey), strings.TrimSpace(item.Value)
		if key != "" && value != "" {
			if previous, exists := values[key]; exists && previous != value {
				decision.Reason = "evidence_contradictory"
				return decision
			}
			values[key] = value
		}
		citations = append(citations, Citation{ID: item.ID, Source: item.Source, Window: item.Window, Query: item.Query})
	}
	decision.State = "ANSWER"
	decision.Reason = "evidence_available"
	decision.Citations = citations
	return decision
}
