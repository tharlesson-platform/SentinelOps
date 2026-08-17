package workflows

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sentinelops/sentinelops/internal/database"
	"github.com/sentinelops/sentinelops/internal/domain"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const ReleaseValidationTaskQueue = "sentinel-release-validation"

type ValidationInput struct {
	ValidationID string `json:"validationId"`
	ReleaseID    string `json:"releaseId"`
	Mode         string `json:"mode"`
}
type Activities struct {
	Store       *database.Store
	HTTPClient  *http.Client
	DemoBaseURL string
}

func ReleaseValidationWorkflow(ctx workflow.Context, input ValidationInput) error {
	opts := workflow.ActivityOptions{StartToCloseTimeout: 2 * time.Minute, ScheduleToCloseTimeout: 3 * time.Minute, RetryPolicy: &temporal.RetryPolicy{InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumInterval: 15 * time.Second, MaximumAttempts: 3}}
	ctx = workflow.WithActivityOptions(ctx, opts)
	return workflow.ExecuteActivity(ctx, "EvaluateValidation", input).Get(ctx, nil)
}

func (a *Activities) EvaluateValidation(ctx context.Context, input ValidationInput) error {
	release, err := a.Store.GetRelease(ctx, input.ReleaseID)
	if err != nil {
		return err
	}
	base := a.DemoBaseURL
	if base == "" {
		base = "http://demo-api:8090"
	}
	endpoint := base + "/health"
	if release.Labels["health_url"] != "" {
		endpoint = release.Labels["health_url"]
	}
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Synthetic-Test", "sentinelops-release-validation")
	resp, probeErr := a.HTTPClient.Do(req)
	duration := time.Since(started)
	status := "PASS"
	message := "health endpoint respondeu dentro do threshold"
	observed := map[string]any{"durationMs": duration.Milliseconds(), "url": endpoint}
	if probeErr != nil {
		status = "INCONCLUSIVE"
		message = "probe sem evidência suficiente: " + probeErr.Error()
	} else {
		observed["httpStatus"] = resp.StatusCode
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			status = "FAIL"
			message = fmt.Sprintf("status HTTP %d fora do intervalo 2xx-3xx", resp.StatusCode)
		}
	}
	checks := []domain.ValidationCheck{{Name: "synthetic-http", Status: status, Required: true, Observed: observed, Threshold: map[string]any{"httpStatus": "200-399", "timeoutMs": 10000}, Message: message}}
	result := status
	if result == "PASS" && duration > 10*time.Second {
		result = "FAIL"
		checks[0].Status = "FAIL"
		checks[0].Message = "latência excedeu 10 segundos"
	}
	return a.Store.CompleteValidation(ctx, input.ValidationID, result, checks[0].Message, checks)
}
