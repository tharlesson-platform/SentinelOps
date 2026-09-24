package telemetryquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExplorerBoundsEvenWhenSourceIgnoresLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "41" || r.URL.Query().Get("timeout") != "8s" {
			t.Error("missing source bounds")
		}
		rows := []any{}
		for i := 0; i < 42; i++ {
			values := []any{}
			for j := 0; j < 400; j++ {
				values = append(values, []any{1000 + j*15, "1"})
			}
			rows = append(rows, map[string]any{"metric": map[string]string{"instance": fmt.Sprint(i)}, "values": values})
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "warnings": []string{"source warning"}, "data": map[string]any{"resultType": "matrix", "result": rows}})
	}))
	defer srv.Close()
	c := &Client{httpClient: srv.Client(), backends: map[string]string{"prometheus": srv.URL}}
	result, err := c.BoundedQuery(context.Background(), "a", "prometheus", "up", time.Unix(1000, 0), time.Unix(2000, 0), 15*time.Second, false)
	if err != nil || !result.Truncated || len(result.Series) != 40 || len(result.Series[0].Points) != 360 || len(result.Warnings) != 1 {
		t.Fatalf("limits: %d %v %v", len(result.Series), result.Truncated, err)
	}
}
func TestDiscoveryPermissionAndDeadlineRemainDistinct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "blocked" {
			w.WriteHeader(403)
			return
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := &Client{httpClient: srv.Client(), backends: map[string]string{"prometheus": srv.URL}}
	_, err := c.BoundedQuery(context.Background(), "a", "prometheus", "blocked", time.Now(), time.Now(), time.Second, true)
	var backend *BackendError
	if !errors.As(err, &backend) || backend.Status != 403 {
		t.Fatalf("permission not typed %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	before := time.Now()
	_, err = c.BoundedQuery(ctx, "a", "prometheus", "slow", time.Now(), time.Now(), time.Second, true)
	if err == nil || time.Since(before) > time.Second {
		t.Fatalf("deadline ignored %v", err)
	}
}
func TestLokiNumericResultsAndMissingValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/query_range" {
			t.Error(r.URL.Path)
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"service_name":"same-name","host_name":"a"},"values":[[1000,"0"],[1015,"NaN"],[1030,"-2"]]},{"metric":{"service_name":"same-name","host_name":"b"},"values":[[1000,"3"]]}]}}`)
	}))
	defer srv.Close()
	c := &Client{httpClient: srv.Client(), backends: map[string]string{"loki": srv.URL}}
	got, err := c.BoundedQuery(context.Background(), "a", "loki", "sum(count_over_time({job=\"x\"}[5m]))", time.Unix(1000, 0), time.Unix(1100, 0), 15*time.Second, false)
	if err != nil || got.Nonfinite != 1 || len(got.Series) != 2 || len(got.Series[0].Points) != 2 || got.Series[0].Points[0].Value != 0 || got.Series[0].Points[1].Value != -2 {
		t.Fatalf("numeric LogQL %v %v", got, err)
	}
}
