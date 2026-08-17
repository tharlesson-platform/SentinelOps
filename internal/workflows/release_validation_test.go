package workflows

import (
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestWorkflowSchedulesActivity(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(ValidationInput) error { return nil }, activity.RegisterOptions{Name: "EvaluateValidation"})
	env.ExecuteWorkflow(ReleaseValidationWorkflow, ValidationInput{ValidationID: "v", ReleaseID: "r", Mode: "smoke"})
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow failed: %v", env.GetWorkflowError())
	}
}
