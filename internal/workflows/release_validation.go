package workflows

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sentinelops/sentinelops/internal/database"
	"github.com/sentinelops/sentinelops/internal/domain"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const ReleaseValidationTaskQueue = "sentinel-release-validation"

type ValidationInput struct {
	OrganizationID string `json:"organizationId"`
	ValidationID   string `json:"validationId"`
	ReleaseID      string `json:"releaseId"`
	Mode           string `json:"mode"`
}
type Activities struct {
	Store              *database.Store
	HTTPClient         *http.Client
	DemoBaseURL        string
	AllowedHealthHosts []string
	PrometheusURL      string
	LokiURL            string
	TempoURL           string
}

func ReleaseValidationWorkflow(ctx workflow.Context, input ValidationInput) error {
	opts := workflow.ActivityOptions{StartToCloseTimeout: 2 * time.Minute, ScheduleToCloseTimeout: 3 * time.Minute, RetryPolicy: &temporal.RetryPolicy{InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumInterval: 15 * time.Second, MaximumAttempts: 3}}
	ctx = workflow.WithActivityOptions(ctx, opts)
	return workflow.ExecuteActivity(ctx, "EvaluateValidation", input).Get(ctx, nil)
}

func (a *Activities) EvaluateValidation(ctx context.Context, input ValidationInput) error {
	if input.OrganizationID == "" {
		return fmt.Errorf("organizationId is required")
	}
	release, err := a.Store.GetRelease(ctx, input.OrganizationID, input.ReleaseID)
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
	if err := validateProbeURL(endpoint, a.AllowedHealthHosts); err != nil {
		checks := []domain.ValidationCheck{{Name: "synthetic-http", Status: "FAIL", Required: true, Observed: map[string]any{"url": endpoint}, Threshold: map[string]any{"allowedHosts": a.AllowedHealthHosts}, Message: "endpoint rejeitado pela política SSRF: " + err.Error()}}
		return a.Store.CompleteValidation(ctx, input.OrganizationID, input.ValidationID, "FAIL", checks[0].Message, checks)
	}
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Synthetic-Test", "sentinelops-release-validation")
	client := *a.HTTPClient
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		return validateProbeURL(req.URL.String(), a.AllowedHealthHosts)
	}
	resp, probeErr := client.Do(req)
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
	checks := []domain.ValidationCheck{{Name: "synthetic-http", Source: "synthetic", Status: status, Required: true, Observed: observed, Threshold: map[string]any{"httpStatus": "200-399", "timeoutMs": 10000}, Message: message}}
	if input.Mode != "smoke" {
		checks = append(checks, a.evaluateTelemetryGates(ctx, input.OrganizationID, release)...)
	}
	result := validationResult(checks)
	if result == "PASS" && duration > 10*time.Second {
		checks[0].Status = "FAIL"
		checks[0].Message = "latência excedeu 10 segundos"
		result = "FAIL"
	}
	return a.Store.CompleteValidation(ctx, input.OrganizationID, input.ValidationID, result, validationSummary(result, checks), checks)
}

func validateProbeURL(raw string, allowedHosts []string) error {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return fmt.Errorf("URL absoluta inválida")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("esquema %q não permitido", u.Scheme)
	}
	if u.User != nil || u.Hostname() == "" {
		return fmt.Errorf("userinfo ou host ausente")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	for _, candidate := range allowedHosts {
		candidate = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(candidate, ".")))
		if candidate == host {
			return nil
		}
		if strings.HasPrefix(candidate, "*.") {
			suffix := strings.TrimPrefix(candidate, "*")
			if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
				return nil
			}
		}
	}
	return fmt.Errorf("host %q fora da allowlist", host)
}
