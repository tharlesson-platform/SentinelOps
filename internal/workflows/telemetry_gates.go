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
	Name       string
	Source     string
	BaseURL    string
	Path       string
	QueryKey   string
	Prefix     string
	DefaultMax float64
}

func (a *Activities) evaluateTelemetryGates(ctx context.Context, organizationID string, release domain.Release) []domain.ValidationCheck {
	specs := []metricGateSpec{
		{Name: "promql", Source: "prometheus", BaseURL: a.PrometheusURL, Path: "/api/v1/query", QueryKey: "query", Prefix: "gate_promql"},
		{Name: "logql", Source: "loki", BaseURL: a.LokiURL, Path: "/loki/api/v1/query", QueryKey: "query", Prefix: "gate_logql"},
		{Name: "slo-burn-rate", Source: "prometheus", BaseURL: a.PrometheusURL, Path: "/api/v1/query", QueryKey: "query", Prefix: "gate_slo_promql", DefaultMax: 1},
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
	maxValue, maxErr := labelFloat(labels, spec.Prefix+"_max", spec.DefaultMax)
	minSamples, samplesErr := labelInt(labels, spec.Prefix+"_min_samples", 1)
	threshold := map[string]any{"max": maxValue, "minSamples": minSamples}
	if query == "" || maxErr != nil || samplesErr != nil || minSamples < 1 {
		return inconclusiveGate(spec.Name, spec.Source, threshold, "política ausente ou inválida; são obrigatórios query, max numérico e min_samples >= 1")
	}
	endpoint, err := fixedQueryURL(spec.BaseURL, spec.Path, spec.QueryKey, query)
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
	values, err := parsePrometheusValues(body)
	if err != nil {
		return inconclusiveGate(spec.Name, spec.Source, threshold, "resposta inválida: "+err.Error())
	}
	observedMax := math.Inf(-1)
	for _, value := range values {
		if value > observedMax {
			observedMax = value
		}
	}
	observed := map[string]any{"query": query, "samples": len(values)}
	if len(values) > 0 {
		observed["max"] = observedMax
	}
	if len(values) < minSamples {
		return domain.ValidationCheck{Name: spec.Name, Source: spec.Source, Required: true, Status: "INCONCLUSIVE", Observed: observed, Threshold: threshold, Message: "amostras insuficientes; ausência de evidência não aprova release"}
	}
	if math.IsNaN(observedMax) || math.IsInf(observedMax, 0) || observedMax > maxValue {
		return domain.ValidationCheck{Name: spec.Name, Source: spec.Source, Required: true, Status: "FAIL", Observed: observed, Threshold: threshold, Message: "valor observado excedeu o limite"}
	}
	return domain.ValidationCheck{Name: spec.Name, Source: spec.Source, Required: true, Status: "PASS", Observed: observed, Threshold: threshold, Message: "query dentro do limite e com amostragem suficiente"}
}

func (a *Activities) evaluateTraceGate(ctx context.Context, organizationID string, labels map[string]string) domain.ValidationCheck {
	query := strings.TrimSpace(labels["gate_traceql"])
	maxMatches, maxErr := labelInt(labels, "gate_traceql_max_matches", 0)
	threshold := map[string]any{"maxMatches": maxMatches, "window": "5m"}
	if query == "" || maxErr != nil || maxMatches < 0 {
		return inconclusiveGate("traceql", "tempo", threshold, "política ausente ou inválida; gate_traceql e gate_traceql_max_matches >= 0 são obrigatórios")
	}
	base, err := url.Parse(strings.TrimRight(a.TempoURL, "/"))
	if err != nil || !base.IsAbs() || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return inconclusiveGate("traceql", "tempo", threshold, "datasource Tempo inválido")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/search"
	q := base.Query()
	now := time.Now().UTC()
	q.Set("q", query)
	q.Set("start", strconv.FormatInt(now.Add(-5*time.Minute).Unix(), 10))
	q.Set("end", strconv.FormatInt(now.Unix(), 10))
	q.Set("limit", strconv.Itoa(maxMatches+100))
	base.RawQuery = q.Encode()
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, base.String(), nil)
	req.Header.Set("X-Scope-OrgID", organizationID)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return inconclusiveGate("traceql", "tempo", threshold, "query sem evidência: "+err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return inconclusiveGate("traceql", "tempo", threshold, fmt.Sprintf("Tempo respondeu HTTP %d", resp.StatusCode))
	}
	var payload struct {
		Traces []json.RawMessage `json:"traces"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Traces == nil {
		return inconclusiveGate("traceql", "tempo", threshold, "resposta TraceQL inválida")
	}
	observed := map[string]any{"query": query, "matches": len(payload.Traces)}
	if len(payload.Traces) > maxMatches {
		return domain.ValidationCheck{Name: "traceql", Source: "tempo", Required: true, Status: "FAIL", Observed: observed, Threshold: threshold, Message: "traces correspondentes excederam o limite"}
	}
	return domain.ValidationCheck{Name: "traceql", Source: "tempo", Required: true, Status: "PASS", Observed: observed, Threshold: threshold, Message: "TraceQL dentro do limite"}
}

func fixedQueryURL(baseURL, path, key, query string) (string, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || !base.IsAbs() || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return "", fmt.Errorf("URL absoluta HTTP(S) obrigatória")
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	values := base.Query()
	values.Set(key, query)
	values.Set("time", strconv.FormatInt(time.Now().UTC().Unix(), 10))
	base.RawQuery = values.Encode()
	return base.String(), nil
}

func parsePrometheusValues(body []byte) ([]float64, error) {
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Status != "success" {
		return nil, fmt.Errorf("envelope de query inválido")
	}
	values := make([]float64, 0, len(payload.Data.Result))
	for _, item := range payload.Data.Result {
		if len(item.Value) != 2 {
			return nil, fmt.Errorf("amostra sem par timestamp/valor")
		}
		var raw string
		if err := json.Unmarshal(item.Value[1], &raw); err != nil {
			return nil, fmt.Errorf("valor não numérico")
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("valor não numérico")
		}
		values = append(values, value)
	}
	return values, nil
}

func labelFloat(labels map[string]string, key string, fallback float64) (float64, error) {
	if strings.TrimSpace(labels[key]) == "" {
		return fallback, nil
	}
	return strconv.ParseFloat(labels[key], 64)
}

func labelInt(labels map[string]string, key string, fallback int) (int, error) {
	if strings.TrimSpace(labels[key]) == "" {
		return fallback, nil
	}
	return strconv.Atoi(labels[key])
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
