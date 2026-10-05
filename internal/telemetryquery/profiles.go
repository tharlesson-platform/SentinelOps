package telemetryquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
)

type ProfileType struct {
	ID         string `json:"ID"`
	Name       string `json:"name"`
	SampleType string `json:"sampleType"`
	SampleUnit string `json:"sampleUnit"`
}
type ProfileFrame struct {
	Name   string `json:"name"`
	Depth  int    `json:"depth"`
	Offset string `json:"offset"`
	Total  string `json:"total"`
	Self   string `json:"self"`
}
type ProfileGraph struct {
	Total   string         `json:"total"`
	Frames  []ProfileFrame `json:"frames"`
	Limited bool           `json:"limited"`
}

// Only fixed query RPCs are supported. There is no arbitrary method, URL or ingestion proxy.
func (c *Client) profileRPC(ctx context.Context, org, method string, body, target any) error {
	switch method {
	case "ProfileTypes", "LabelNames", "LabelValues", "SelectMergeStacktraces":
	default:
		return errors.New("unsupported profile RPC")
	}
	endpoint := c.backends["pyroscope"]
	if c.gateway != "" {
		endpoint = c.gateway + "/pyroscope"
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/querier.v1.QuerierService/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if c.gateway != "" {
		req.Header.Set("X-Sentinel-Organization", org)
	} else {
		req.Header.Set("X-Scope-OrgID", org)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponseBytes {
		return errors.New("profile response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &BackendError{Source: "pyroscope", Status: resp.StatusCode}
	}
	return json.Unmarshal(data, target)
}
func (c *Client) ProfileTypes(ctx context.Context, org string, start, end time.Time) ([]ProfileType, error) {
	var out struct {
		Types []ProfileType `json:"profileTypes"`
	}
	err := c.profileRPC(ctx, org, "ProfileTypes", map[string]int64{"start": start.UnixMilli(), "end": end.UnixMilli()}, &out)
	if len(out.Types) > 100 {
		return nil, errors.New("too many profile types")
	}
	return out.Types, err
}
func (c *Client) ProfileLabels(ctx context.Context, org, label, selector string, start, end time.Time) ([]string, bool, error) {
	body := map[string]any{"start": start.UnixMilli(), "end": end.UnixMilli()}
	method := "LabelNames"
	if label != "" {
		method = "LabelValues"
		body["name"] = label
	}
	if selector != "" {
		body["matchers"] = []string{selector}
	}
	var out struct {
		Names []string `json:"names"`
	}
	err := c.profileRPC(ctx, org, method, body, &out)
	truncated := len(out.Names) > DiscoveryLimit
	if truncated {
		out.Names = out.Names[:DiscoveryLimit]
	}
	return out.Names, truncated, err
}
func (c *Client) ProfileFlamegraph(ctx context.Context, org, profileType, selector string, start, end time.Time) (ProfileGraph, error) {
	var out struct {
		Flamegraph struct {
			Names  []string `json:"names"`
			Levels []struct {
				Values []json.Number `json:"values"`
			} `json:"levels"`
			Total json.Number `json:"total"`
		} `json:"flamegraph"`
	}
	err := c.profileRPC(ctx, org, "SelectMergeStacktraces", map[string]any{"profileTypeID": profileType, "labelSelector": selector, "start": start.UnixMilli(), "end": end.UnixMilli(), "maxNodes": 200}, &out)
	graph := ProfileGraph{Total: "0", Frames: []ProfileFrame{}, Limited: true}
	if err != nil {
		return graph, err
	}
	total, err := out.Flamegraph.Total.Int64()
	if err != nil && len(out.Flamegraph.Levels) > 0 {
		return graph, err
	}
	if total < 0 {
		return graph, errors.New("negative profile total")
	}
	graph.Total = out.Flamegraph.Total.String()
	if graph.Total == "" {
		graph.Total = "0"
	}
	for depth, level := range out.Flamegraph.Levels {
		if len(level.Values)%4 != 0 {
			return graph, errors.New("invalid flamegraph level")
		}
		var previous int64
		for i := 0; i < len(level.Values); i += 4 {
			if len(graph.Frames) >= 500 {
				return ProfileGraph{}, errors.New("profile node bound exceeded")
			}
			nums := [4]int64{}
			for j := 0; j < 4; j++ {
				nums[j], err = level.Values[i+j].Int64()
				if err != nil || nums[j] < 0 {
					return graph, errors.New("invalid flamegraph value")
				}
			}
			offset := previous + nums[0]
			previous = offset + nums[1]
			if offset < 0 || previous < offset || previous > total || nums[2] > nums[1] || nums[3] >= int64(len(out.Flamegraph.Names)) {
				return graph, errors.New("invalid flamegraph bounds")
			}
			graph.Frames = append(graph.Frames, ProfileFrame{Name: out.Flamegraph.Names[nums[3]], Depth: depth, Offset: strconv.FormatInt(offset, 10), Total: level.Values[i+1].String(), Self: level.Values[i+2].String()})
		}
	}
	return graph, nil
}
