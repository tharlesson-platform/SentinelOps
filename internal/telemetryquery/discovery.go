package telemetryquery

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strconv"
	"time"
)

const ExplorerSeriesLimit = 40
const DiscoveryLimit = 2000

type MetricMetadata struct {
	Type string `json:"type"`
	Help string `json:"help"`
	Unit string `json:"unit"`
}
type QueryResult struct {
	Type      string       `json:"type"`
	Series    []TimeSeries `json:"series"`
	Logs      []LogEntry   `json:"logs,omitempty"`
	Warnings  []string     `json:"warnings,omitempty"`
	Truncated bool         `json:"truncated"`
	Nonfinite int          `json:"nonfinite"`
}

// Discovery is restricted to fixed read-only endpoints. The caller never supplies a URL.
func (c *Client) MetricNames(ctx context.Context, org string, start, end time.Time) ([]string, error) {
	var out struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	err := c.getJSON(ctx, "prometheus", "/api/v1/label/__name__/values", url.Values{"start": {formatSeconds(start)}, "end": {formatSeconds(end)}, "limit": {"10001"}}, org, &out)
	if err == nil && out.Status != "success" {
		err = errors.New("invalid metric names response")
	}
	if len(out.Data) > 10001 {
		out.Data = out.Data[:10001]
	}
	return out.Data, err
}
func (c *Client) Metadata(ctx context.Context, org string) (map[string][]MetricMetadata, error) {
	var out struct {
		Status string                      `json:"status"`
		Data   map[string][]MetricMetadata `json:"data"`
	}
	err := c.getJSON(ctx, "prometheus", "/api/v1/metadata", url.Values{"limit": {"10001"}, "limit_per_metric": {"2"}}, org, &out)
	if err == nil && out.Status != "success" {
		err = errors.New("invalid metadata response")
	}
	return out.Data, err
}
func (c *Client) LabelValues(ctx context.Context, org, source, label, selector string, start, end time.Time) ([]string, bool, error) {
	var out struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	p := url.Values{"start": {formatSeconds(start)}, "end": {formatSeconds(end)}, "limit": {strconv.Itoa(DiscoveryLimit + 1)}}
	path := "/api/v1/label/" + url.PathEscape(label) + "/values"
	if source == "loki" {
		path = "/loki" + path
		p.Set("query", selector)
		p.Set("start", strconv.FormatInt(start.UnixNano(), 10))
		p.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	} else if selector != "" {
		p.Set("match[]", selector)
	}
	err := c.getJSON(ctx, source, path, p, org, &out)
	if err == nil && out.Status != "success" {
		err = errors.New("invalid label response")
	}
	truncated := len(out.Data) > DiscoveryLimit
	if truncated {
		out.Data = out.Data[:DiscoveryLimit]
	}
	return out.Data, truncated, err
}
func (c *Client) MetricSeries(ctx context.Context, org, selector string, start, end time.Time) ([]map[string]string, bool, error) {
	var out struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	err := c.getJSON(ctx, "prometheus", "/api/v1/series", url.Values{"match[]": {selector}, "start": {formatSeconds(start)}, "end": {formatSeconds(end)}, "limit": {"501"}}, org, &out)
	if err == nil && out.Status != "success" {
		err = errors.New("invalid series response")
	}
	truncated := len(out.Data) > 500
	if truncated {
		out.Data = out.Data[:500]
	}
	return out.Data, truncated, err
}

// BoundedQuery supports PromQL and the numeric or stream results of trusted LogQL templates.
func (c *Client) BoundedQuery(ctx context.Context, org, source, query string, start, end time.Time, step time.Duration, instant bool) (QueryResult, error) {
	result := QueryResult{Series: []TimeSeries{}}
	p := url.Values{"query": {query}, "start": {formatSeconds(start)}, "end": {formatSeconds(end)}, "step": {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)}, "timeout": {"8s"}, "limit": {strconv.Itoa(ExplorerSeriesLimit + 1)}}
	path := "/api/v1/query_range"
	if instant {
		path = "/api/v1/query"
		p.Set("time", formatSeconds(end))
		p.Del("start")
		p.Del("end")
		p.Del("step")
	}
	if source == "loki" {
		path = "/loki" + path
		p.Set("limit", "200")
		p.Set("direction", "backward")
		if !instant {
			p.Set("start", strconv.FormatInt(start.UnixNano(), 10))
			p.Set("end", strconv.FormatInt(end.UnixNano(), 10))
		}
	}
	var envelope struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Data     struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, source, path, p, org, &envelope); err != nil {
		return result, err
	}
	if envelope.Status != "success" {
		return result, errors.New("source query failed")
	}
	result.Type = envelope.Data.ResultType
	result.Warnings = envelope.Warnings
	if result.Type == "scalar" {
		var raw []json.RawMessage
		if err := json.Unmarshal(envelope.Data.Result, &raw); err != nil {
			return result, err
		}
		point, err := decodePoint(raw)
		if err != nil {
			return result, err
		}
		if math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
			result.Nonfinite++
		} else {
			result.Series = append(result.Series, TimeSeries{Labels: map[string]string{}, Points: []Point{point}})
		}
		return result, nil
	}
	if result.Type == "streams" {
		var streams []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		}
		if err := json.Unmarshal(envelope.Data.Result, &streams); err != nil {
			return result, err
		}
		for _, s := range streams {
			for _, v := range s.Values {
				if len(v) != 2 {
					continue
				}
				ns, e := strconv.ParseInt(v[0], 10, 64)
				if e != nil {
					continue
				}
				if len(result.Logs) >= 200 {
					result.Truncated = true
					continue
				}
				result.Logs = append(result.Logs, LogEntry{Timestamp: time.Unix(0, ns).UTC(), Line: v[1], Labels: s.Stream})
			}
		}
		result.Truncated = len(result.Logs) >= 200
		return result, nil
	}
	if result.Type != "vector" && result.Type != "matrix" {
		return result, errors.New("unsupported source result type")
	}
	var samples []struct {
		Metric     map[string]string   `json:"metric"`
		Values     [][]json.RawMessage `json:"values"`
		Value      []json.RawMessage   `json:"value"`
		Histogram  json.RawMessage     `json:"histogram"`
		Histograms json.RawMessage     `json:"histograms"`
	}
	if err := json.Unmarshal(envelope.Data.Result, &samples); err != nil {
		return result, err
	}
	result.Truncated = len(samples) > ExplorerSeriesLimit
	if result.Truncated {
		samples = samples[:ExplorerSeriesLimit]
	}
	for _, s := range samples {
		if len(s.Histogram) > 0 || len(s.Histograms) > 0 {
			result.Warnings = append(result.Warnings, "Histogramas nativos não são convertidos em valores escalares; consulte uma agregação explícita.")
		}
		values := s.Values
		if result.Type == "vector" {
			if len(s.Value) > 0 {
				values = [][]json.RawMessage{s.Value}
			}
		}
		if len(values) > 360 {
			result.Truncated = true
			values = values[:360]
		}
		series := TimeSeries{Labels: s.Metric, Points: []Point{}}
		for _, raw := range values {
			point, err := decodePoint(raw)
			if err != nil {
				return result, err
			}
			if math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
				result.Nonfinite++
				continue
			}
			series.Points = append(series.Points, point)
		}
		result.Series = append(result.Series, series)
	}
	return result, nil
}

// PlatformInventory omits discovered labels, scrape URLs, annotations and credentials.
func (c *Client) PlatformInventory(ctx context.Context, org string) (map[string]any, error) {
	var targets struct {
		Status string `json:"status"`
		Data   struct {
			ActiveTargets []struct {
				Labels     map[string]string `json:"labels"`
				Health     string            `json:"health"`
				LastScrape string            `json:"lastScrape"`
				LastError  string            `json:"lastError"`
			} `json:"activeTargets"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, "prometheus", "/api/v1/targets", url.Values{"state": {"active"}}, org, &targets); err != nil {
		return nil, err
	}
	var rules struct {
		Status string `json:"status"`
		Data   struct {
			Groups []struct {
				Name  string `json:"name"`
				Rules []struct {
					Name   string            `json:"name"`
					Type   string            `json:"type"`
					Query  string            `json:"query"`
					Health string            `json:"health"`
					State  string            `json:"state"`
					Labels map[string]string `json:"labels"`
				} `json:"rules"`
			} `json:"groups"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, "prometheus", "/api/v1/rules", nil, org, &rules); err != nil {
		return nil, err
	}
	if targets.Status != "success" || rules.Status != "success" {
		return nil, errors.New("invalid platform inventory")
	}
	rows := []map[string]any{}
	for _, t := range targets.Data.ActiveTargets {
		rows = append(rows, map[string]any{"job": t.Labels["job"], "instance": t.Labels["instance"], "health": t.Health, "lastScrape": t.LastScrape, "hasError": t.LastError != ""})
	}
	return map[string]any{"targets": rows, "groups": rules.Data.Groups}, nil
}
