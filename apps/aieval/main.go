// sentinelops-aieval runs deterministic, sanitized guardrail fixtures. It is
// intentionally not an LLM benchmark and never contacts a provider.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sentinelops/sentinelops/internal/assistant"
)

type evaluationCase struct {
	ID             string            `json:"id"`
	Request        assistant.Request `json:"request"`
	ExpectedState  string            `json:"expectedState"`
	ExpectedReason string            `json:"expectedReason"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "use: sentinelops-aieval <fixtures.json>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read fixtures: %v\n", err)
		os.Exit(2)
	}
	var cases []evaluationCase
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases) < 30 {
		fmt.Fprintln(os.Stderr, "fixtures must contain at least 30 valid cases")
		os.Exit(2)
	}
	failed := 0
	for _, item := range cases {
		decision := assistant.Evaluate(item.Request)
		if item.ID == "" || decision.State != item.ExpectedState || decision.Reason != item.ExpectedReason || len(decision.ToolCalls) != 0 {
			fmt.Fprintf(os.Stderr, "FAIL %s: got %s/%s\n", item.ID, decision.State, decision.Reason)
			failed++
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
	fmt.Printf("PASS: %d cenarios de guardrail; nenhum provider ou tool foi chamado\n", len(cases))
}
