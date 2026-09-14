package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sentinelops/sentinelops/internal/domain"
)

type metricGateSpec struct {
	Name    string
	Source  string
	BaseURL string
	Path    string
	Prefix  string
}

type telemetrySample struct {
	At    time.Time
	Value float64
}

func (a *Activities) evaluateTelemetryGates(ctx context.Context, organizationID string, release domain.Release) []domain.ValidationCheck {
	specs := []metricGateSpec{
		{Name: "promql", Source: "prometheus", BaseURL: a.PrometheusURL, Path: "/api/v1/query_range", Prefix: "gate_promql"},
		{Name: "logql", Source: "loki", BaseURL: a.LokiURL, Path: "/loki/api/v1/query_range", Prefix: "gate_logql"},
		{Name: "slo-burn-rate", Source: "prometheus", BaseURL: a.PrometheusURL, Path: "/api/v1/query_range", Prefix: "gate_slo_promql"},
	}
	checks := make([]domain.ValidationCheck, 0, 4)
	for _, spec := range specs {
		checks = append(checks, a.evaluateMetricGate(ctx, organizationID, release.Labels, spec))
	}
	checks = append(checks, a.evaluateTraceGate(ctx, organizationID, release.Labels))
	return checks
}

func (a *Activities) evaluateMetricGate(ctx context.Context, organizationID string, labels map[string]string, spec metricGateSpec) domain.ValidationCheck {
	query := strings.TrimSpace(labels[spec.Prefix])
	maxValue, maxErr := labelFiniteFloat(labels, spec.Prefix+"_max")
	minSamples, samplesErr := labelInt(labels, spec.Prefix+"_min_samples", 0)
	window, windowErr := labelDuration(labels, spec.Prefix+"_window", 0)
	maxAge, ageErr := labelDuration(labels, spec.Prefix+"_max_age", 0)
	step, stepErr := labelDuration(labels, spec.Prefix+"_step", 0)
	threshold := map[string]any{"max": maxValue, "minSamples": minSamples, "window": window.String(), "maxAge": maxAge.String(), "step": step.String()}
	if query == "" || maxErr != nil || samplesErr != nil || minSamples < 1 || windowErr != nil || ageErr != nil || stepErr != nil || window <= 0 || maxAge <= 0 || step <= 0 || step > window {
		return inconclusiveGate(spec.Name, spec.Source, threshold, "política ausente ou inválida; são obrigatórios query, max finito, min_samples, window, max_age e step")
	}
	now := time.Now().UTC()
	endpoint, err := rangeQueryURL(spec.BaseURL, spec.Path, query, now.Add(-window), now, step)
	if err != nil {
		return inconclusiveGate(spec.Name, spec.Source, threshold, "datasource inválido: "+err.Error())
	}
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	req.Header.Set("X-Scope-OrgID", organizationID)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return inconclusiveGate(spec.Name, spec.Source, threshold, "query sem evidência: "+err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return inconclusiveGate(spec.Name, spec.Source, threshold, fmt.Sprintf("datasource respondeu HTTP %d", resp.StatusCode))
	}
	samples, warnings, err := parsePrometheusRangeSamples(body)
	if err != nil {
		return inconclusiveGate(spec.Name, spec.Source, threshold, "resposta inválida: "+err.Error())
	}
	if len(warnings) > 0 {
		return inconclusiveGate(spec.Name, spec.Source, threshold, "consulta parcial ou com warning do datasource")
	}

	observedMax := math.Inf(-1)
	newest := time.Time{}
	for _, sample := range samples {
		if sample.At.Before(now.Add(-window).Add(-step)) || sample.At.After(now.Add(step)) {
			return inconclusiveGate(spec.Name, spec.Source, threshold, "amostra fora da janela solicitada")
		}
		if math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) {
			return inconclusiveGate(spec.Name, spec.Source, threshold, "amostra não-finita; datasource não forneceu evidência utilizável")
		}
		if sample.Value > observedMax {
			observedMax = sample.Value
		}
		if sample.At.After(newest) {
			newest = sample.At
		}
	}
	observed := map[string]any{"query": query, "samples": len(samples)}
	if !newest.IsZero() {
		observed["newestAt"] = newest.Format(time.RFC3339Nano)
		observed["freshnessMs"] = now.Sub(newest).Milliseconds()
	}
	if len(samples) < minSamples {
		return domain.ValidationCheck{Name: spec.Name, Source: spec.Source, Required: true, Status: "INCONCLUSIVE", Observed: observed, Threshold: threshold, Message: "amostras temporais insuficientes; ausência de evidência não aprova release"}
	}
	if newest.Before(now.Add(-maxAge)) {
		return domain.ValidationCheck{Name: spec.Name, Source: spec.Source, Required: true, Status: "INCONCLUSIVE", Observed: observed, Threshold: threshold, Message: "evidência obsoleta para a janela de validação"}
	}
	observed["max"] = observedMax
	if observedMax > maxValue {
		return domain.ValidationCheck{Name: spec.Name, Source: spec.Source, Required: true, Status: "FAIL", Observed: observed, Threshold: threshold, Message: "valor observado excedeu o limite"}
	}
	return domain.ValidationCheck{Name: spec.Name, Source: spec.Source, Required: true, Status: "PASS", Observed: observed, Threshold: threshold, Message: "janela consultada dentro do limite, com amostragem e freshness suficientes"}
}

func (a *Activities) evaluateTraceGate(ctx context.Context, organizationID string, labels map[string]string) domain.ValidationCheck {
	query := strings.TrimSpace(labels["gate_traceql"])
	coverageQuery := strings.TrimSpace(labels["gate_traceql_coverage_query"])
	maxMatches, maxErr := labelInt(labels, "gate_traceql_max_matches", -1)
	minCoverage, coverageErr := labelInt(labels, "gate_traceql_min_coverage_matches", 0)
	window, windowErr := labelDuration(labels, "gate_traceql_window", 0)
	threshold := map[string]any{"maxMatches": maxMatches, "minCoverageMatches": minCoverage, "window": window.String()}
	if query == "" || coverageQuery == "" || maxErr != nil || coverageErr != nil || windowErr != nil || maxMatches < 0 || minCoverage < 1 || window <= 0 {
		return inconclusiveGate("traceql", "tempo", threshold, "política ausente ou inválida; são obrigatórios traceql, coverage_query, max_matches, min_coverage_matches e window")
	}
	errorMatches, errorWarnings, err := a.traceMatches(ctx, organizationID, query, window, maxMatches+1)
	if err != nil {
		return inconclusiveGate("traceql", "tempo", threshold, "query sem evidência: "+err.Error())
	}
	if len(errorWarnings) > 0 {
		return inconclusiveGate("traceql", "tempo", threshold, "consulta TraceQL parcial ou com warning")
	}
	coverageMatches, coverageWarnings, err := a.traceMatches(ctx, organizationID, coverageQuery, window, minCoverage)
	if err != nil {
		return inconclusiveGate("traceql", "tempo", threshold, "cobertura de ingestão sem evidência: "+err.Error())
	}
	if len(coverageWarnings) > 0 {
		return inconclusiveGate("traceql", "tempo", threshold, "consulta de cobertura TraceQL parcial ou com warning")
	}
	observed := map[string]any{"query": query, "matches": errorMatches, "coverageQuery": coverageQuery, "coverageMatches": coverageMatches}
	if coverageMatches < minCoverage {
		return domain.ValidationCheck{Name: "traceql", Source: "tempo", Required: true, Status: "INCONCLUSIVE", Observed: observed, Threshold: threshold, Message: "cobertura de traces insuficiente; zero erro não comprova ingestão"}
	}
	if errorMatches > maxMatches {
		return domain.ValidationCheck{Name: "traceql", Source: "tempo", Required: true, Status: "FAIL", Observed: observed, Threshold: threshold, Message: "traces correspondentes excederam o limite"}
	}
	return domain.ValidationCheck{Name: "traceql", Source: "tempo", Required: true, Status: "PASS", Observed: observed, Threshold: threshold, Message: "TraceQL dentro do limite e com cobertura positiva de ingestão"}
}

func (a *Activities) traceMatches(ctx context.Context, organizationID, query string, window time.Duration, limit int) (int, []string, error) {
	base, err := url.Parse(strings.TrimRight(a.TempoURL, "/"))
	if err != nil || !base.IsAbs() || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return 0, nil, fmt.Errorf("datasource Tempo inválido")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/search"
	q := base.Query()
	now := time.Now().UTC()
	q.Set("q", query)
	q.Set("start", strconv.FormatInt(now.Add(-window).Unix(), 10))
	q.Set("end", strconv.FormatInt(now.Unix(), 10))
	q.Set("limit", strconv.Itoa(limit))
	base.RawQuery = q.Encode()
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, base.String(), nil)
	req.Header.Set("X-Scope-OrgID", organizationID)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("Tempo respondeu HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Traces   []json.RawMessage `json:"traces"`
		Warnings []string          `json:"warnings"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Traces == nil {
		return 0, nil, fmt.Errorf("resposta TraceQL inválida")
	}
	return len(payload.Traces), payload.Warnings, nil
}

func rangeQueryURL(baseURL, path, query string, start, end time.Time, step time.Duration) (string, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || !base.IsAbs() || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return "", fmt.Errorf("URL absoluta HTTP(S) obrigatória")
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	values := base.Query()
	values.Set("query", query)
	values.Set("start", strconv.FormatInt(start.Unix(), 10))
	values.Set("end", strconv.FormatInt(end.Unix(), 10))
	values.Set("step", step.String())
	base.RawQuery = values.Encode()
	return base.String(), nil
}

func parsePrometheusRangeSamples(body []byte) ([]telemetrySample, []string, error) {
	var payload struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Data     struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Status != "success" || payload.Data.ResultType != "matrix" {
		return nil, nil, fmt.Errorf("envelope de query_range inválido")
	}
	samples := make([]telemetrySample, 0)
	for _, series := range payload.Data.Result {
		for _, value := range series.Values {
			if len(value) != 2 {
				return nil, nil, fmt.Errorf("amostra sem par timestamp/valor")
			}
			var timestamp float64
			var raw string
			if err := json.Unmarshal(value[0], &timestamp); err != nil {
				return nil, nil, fmt.Errorf("timestamp não numérico")
			}
			if err := json.Unmarshal(value[1], &raw); err != nil {
				return nil, nil, fmt.Errorf("valor não numérico")
			}
			parsed, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("valor não numérico")
			}
			seconds, fraction := math.Modf(timestamp)
			samples = append(samples, telemetrySample{At: time.Unix(int64(seconds), int64(fraction*float64(time.Second))).UTC(), Value: parsed})
		}
	}
	return samples, payload.Warnings, nil
}

func labelFiniteFloat(labels map[string]string, key string) (float64, error) {
	value := strings.TrimSpace(labels[key])
	if value == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%s must be finite", key)
	}
	return parsed, nil
}

func labelInt(labels map[string]string, key string, fallback int) (int, error) {
	if strings.TrimSpace(labels[key]) == "" {
		return fallback, nil
	}
	return strconv.Atoi(labels[key])
}

func labelDuration(labels map[string]string, key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(labels[key])
	if value == "" {
		return fallback, fmt.Errorf("%s is required", key)
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}

func inconclusiveGate(name, source string, threshold any, message string) domain.ValidationCheck {
	return domain.ValidationCheck{Name: name, Source: source, Required: true, Status: "INCONCLUSIVE", Threshold: threshold, Message: message}
}

func validationResult(checks []domain.ValidationCheck) string {
	result := "PASS"
	for _, check := range checks {
		if !check.Required {
			continue
		}
		if check.Status == "FAIL" {
			return "FAIL"
		}
		if check.Status != "PASS" {
			result = "INCONCLUSIVE"
		}
	}
	return result
}

func validationSummary(result string, checks []domain.ValidationCheck) string {
	failed, inconclusive := 0, 0
	for _, check := range checks {
		if check.Status == "FAIL" {
			failed++
		} else if check.Status != "PASS" {
			inconclusive++
		}
	}
	return fmt.Sprintf("%s: %d checks, %d falhas, %d inconclusivos", result, len(checks), failed, inconclusive)
}
