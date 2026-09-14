package telemetryquery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 8 << 20

type Options struct {
	GatewayURL     string
	PrometheusURL  string
	LokiURL        string
	TempoURL       string
	PyroscopeURL   string
	ClientCertFile string
	ClientKeyFile  string
	RequestTimeout time.Duration
}

type Client struct {
	httpClient *http.Client
	gateway    string
	backends   map[string]string
}

type Point struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type VectorSample struct {
	Labels    map[string]string `json:"labels"`
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
}

type TimeSeries struct {
	Labels map[string]string `json:"labels"`
	Points []Point           `json:"points"`
}

type LogEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	Line      string            `json:"line"`
	Labels    map[string]string `json:"labels"`
}

type TraceSummary struct {
	TraceID           string  `json:"traceId"`
	RootServiceName   string  `json:"rootServiceName"`
	RootTraceName     string  `json:"rootTraceName"`
	StartTimeUnixNano string  `json:"startTimeUnixNano"`
	DurationMs        float64 `json:"durationMs"`
}

func New(options Options) (*Client, error) {
	timeout := options.RequestTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if options.ClientCertFile != "" || options.ClientKeyFile != "" {
		if options.ClientCertFile == "" || options.ClientKeyFile == "" {
			return nil, errors.New("telemetry query client certificate and key must be configured together")
		}
		certificate, err := tls.LoadX509KeyPair(options.ClientCertFile, options.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load telemetry query client certificate: %w", err)
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	}
	client := &Client{
		httpClient: &http.Client{Timeout: timeout, Transport: transport},
		gateway:    strings.TrimRight(options.GatewayURL, "/"),
		backends: map[string]string{
			"prometheus": strings.TrimRight(options.PrometheusURL, "/"),
			"loki":       strings.TrimRight(options.LokiURL, "/"),
			"tempo":      strings.TrimRight(options.TempoURL, "/"),
			"pyroscope":  strings.TrimRight(options.PyroscopeURL, "/"),
		},
	}
	for source, endpoint := range client.backends {
		if endpoint == "" && client.gateway == "" {
			return nil, fmt.Errorf("%s telemetry endpoint is not configured", source)
		}
	}
	return client, nil
}

func (c *Client) PrometheusInstant(ctx context.Context, organizationID, query string, at time.Time) ([]VectorSample, error) {
	parameters := url.Values{"query": {query}, "time": {formatSeconds(at)}}
	var envelope prometheusEnvelope
	if err := c.getJSON(ctx, "prometheus", "/api/v1/query", parameters, organizationID, &envelope); err != nil {
		return nil, err
	}
	if envelope.Status != "success" || envelope.Data.ResultType != "vector" {
		return nil, errors.New("prometheus returned an invalid vector envelope")
	}
	result := make([]VectorSample, 0, len(envelope.Data.Result))
	for _, item := range envelope.Data.Result {
		point, err := decodePoint(item.Value)
		if err != nil {
			return nil, fmt.Errorf("decode prometheus vector sample: %w", err)
		}
		result = append(result, VectorSample{Labels: item.Metric, Timestamp: point.Timestamp, Value: point.Value})
	}
	return result, nil
}

func (c *Client) PrometheusRange(ctx context.Context, organizationID, query string, start, end time.Time, step time.Duration) ([]TimeSeries, error) {
	if step < time.Second {
		step = 30 * time.Second
	}
	parameters := url.Values{"query": {query}, "start": {formatSeconds(start)}, "end": {formatSeconds(end)}, "step": {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)}}
	var envelope prometheusEnvelope
	if err := c.getJSON(ctx, "prometheus", "/api/v1/query_range", parameters, organizationID, &envelope); err != nil {
		return nil, err
	}
	if envelope.Status != "success" || envelope.Data.ResultType != "matrix" {
		return nil, errors.New("prometheus returned an invalid matrix envelope")
	}
	result := make([]TimeSeries, 0, len(envelope.Data.Result))
	for _, item := range envelope.Data.Result {
		points := make([]Point, 0, len(item.Values))
		for _, raw := range item.Values {
			point, err := decodePoint(raw)
			if err != nil {
				return nil, fmt.Errorf("decode prometheus range sample: %w", err)
			}
			points = append(points, point)
		}
		result = append(result, TimeSeries{Labels: item.Metric, Points: points})
	}
	return result, nil
}

func (c *Client) LokiRange(ctx context.Context, organizationID, query string, start, end time.Time, limit int, direction string) ([]LogEntry, error) {
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	if direction != "forward" {
		direction = "backward"
	}
	parameters := url.Values{
		"query": {query}, "start": {strconv.FormatInt(start.UnixNano(), 10)}, "end": {strconv.FormatInt(end.UnixNano(), 10)},
		"limit": {strconv.Itoa(limit)}, "direction": {direction},
	}
	var envelope lokiEnvelope
	if err := c.getJSON(ctx, "loki", "/loki/api/v1/query_range", parameters, organizationID, &envelope); err != nil {
		return nil, err
	}
	if envelope.Status != "success" || envelope.Data.ResultType != "streams" {
		return nil, errors.New("loki returned an invalid streams envelope")
	}
	entries := make([]LogEntry, 0)
	for _, stream := range envelope.Data.Result {
		for _, value := range stream.Values {
			if len(value) != 2 {
				continue
			}
			nanoseconds, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil {
				continue
			}
			entries = append(entries, LogEntry{Timestamp: time.Unix(0, nanoseconds).UTC(), Line: value[1], Labels: stream.Stream})
		}
	}
	return entries, nil
}

func (c *Client) TempoSearch(ctx context.Context, organizationID, traceQL string, start, end time.Time, limit int) ([]TraceSummary, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	parameters := url.Values{"q": {traceQL}, "start": {strconv.FormatInt(start.Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)}, "limit": {strconv.Itoa(limit)}}
	var envelope struct {
		Traces []TraceSummary `json:"traces"`
	}
	if err := c.getJSON(ctx, "tempo", "/api/search", parameters, organizationID, &envelope); err != nil {
		return nil, err
	}
	if envelope.Traces == nil {
		envelope.Traces = []TraceSummary{}
	}
	return envelope.Traces, nil
}

func (c *Client) getJSON(ctx context.Context, source, path string, parameters url.Values, organizationID string, target any) error {
	endpoint := c.backends[source]
	if c.gateway != "" {
		endpoint = c.gateway + "/" + source
	}
	requestURL := endpoint + path
	if encoded := parameters.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return fmt.Errorf("build %s query: %w", source, err)
		}
		if c.gateway != "" {
			request.Header.Set("X-Sentinel-Organization", organizationID)
		} else if organizationID != "" {
			request.Header.Set("X-Scope-OrgID", organizationID)
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			lastErr = fmt.Errorf("%s telemetry query failed: %w", source, err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read %s response: %w", source, readErr)
		}
		if len(body) > maxResponseBytes {
			return fmt.Errorf("%s response exceeds %d bytes", source, maxResponseBytes)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = fmt.Errorf("%s telemetry backend returned HTTP %d", source, response.StatusCode)
			if response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusGatewayTimeout {
				continue
			}
			return lastErr
		}
		if err := json.Unmarshal(body, target); err != nil {
			return fmt.Errorf("decode %s telemetry response: %w", source, err)
		}
		return nil
	}
	return lastErr
}

func formatSeconds(value time.Time) string {
	return strconv.FormatFloat(float64(value.UnixNano())/1e9, 'f', 3, 64)
}

func decodePoint(raw []json.RawMessage) (Point, error) {
	if len(raw) != 2 {
		return Point{}, errors.New("sample must contain timestamp and value")
	}
	var timestamp float64
	if err := json.Unmarshal(raw[0], &timestamp); err != nil {
		return Point{}, err
	}
	var value string
	if err := json.Unmarshal(raw[1], &value); err != nil {
		return Point{}, err
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return Point{}, err
	}
	return Point{Timestamp: time.Unix(0, int64(timestamp*1e9)).UTC(), Value: number}, nil
}

type prometheusEnvelope struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string   `json:"metric"`
			Value  []json.RawMessage   `json:"value"`
			Values [][]json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

type lokiEnvelope struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}
