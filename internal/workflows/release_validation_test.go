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

func TestValidateProbeURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		hosts   []string
		wantErr bool
	}{
		{name: "exact host", url: "https://status.example.com/health", hosts: []string{"status.example.com"}},
		{name: "wildcard subdomain", url: "https://api.prod.example.com/health", hosts: []string{"*.prod.example.com"}},
		{name: "metadata blocked", url: "http://169.254.169.254/latest/meta-data", hosts: []string{"demo-api"}, wantErr: true},
		{name: "lookalike blocked", url: "https://status.example.com.attacker.test", hosts: []string{"status.example.com"}, wantErr: true},
		{name: "wildcard apex blocked", url: "https://prod.example.com", hosts: []string{"*.prod.example.com"}, wantErr: true},
		{name: "userinfo blocked", url: "https://user@status.example.com/health", hosts: []string{"status.example.com"}, wantErr: true},
		{name: "non http blocked", url: "file:///etc/passwd", hosts: []string{"demo-api"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProbeURL(tt.url, tt.hosts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateProbeURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
