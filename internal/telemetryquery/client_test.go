package telemetryquery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPrometheusOmitsNonFiniteValuesWithoutFabricatingZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/query_range" {
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"instance":"mixed"},"values":[[1000,"2"],[1015,"NaN"],[1030,"4"],[1045,"+Inf"],[1060,"-Inf"]]},{"metric":{"instance":"missing"},"values":[[1000,"NaN"]]}]}}`)
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"finite-zero"},"value":[1000,"0"]},{"metric":{"instance":"undefined"},"value":[1000,"NaN"]},{"metric":{"instance":"positive-infinity"},"value":[1000,"+Inf"]},{"metric":{"instance":"negative-infinity"},"value":[1000,"-Inf"]}]}}`)
	}))
	defer server.Close()
	client, err := New(Options{PrometheusURL: server.URL, LokiURL: server.URL, TempoURL: server.URL, PyroscopeURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	samples, err := client.PrometheusInstant(context.Background(), "tenant", "fixture", time.Unix(1000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].Value != 0 || samples[0].Labels["instance"] != "finite-zero" {
		t.Fatalf("undefined values became observations: %#v", samples)
	}
	series, err := client.PrometheusRange(context.Background(), "tenant", "fixture", time.Unix(1000, 0), time.Unix(1060, 0), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 || len(series[0].Points) != 2 || len(series[1].Points) != 0 {
		t.Fatalf("finite or missing observations were lost: %#v", series)
	}
	if series[0].Points[1].Timestamp.Sub(series[0].Points[0].Timestamp) != 30*time.Second || series[0].Points[0].Value != 2 || series[0].Points[1].Value != 4 {
		t.Fatal("missing sample must preserve the temporal gap and finite values")
	}
	if _, err := json.Marshal(series); err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusInstantUsesTenantAndParsesVector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" || r.Header.Get("X-Scope-OrgID") != "tenant-a" || r.URL.Query().Get("query") != "up" {
			t.Fatalf("unexpected request: %s tenant=%q query=%q", r.URL.Path, r.Header.Get("X-Scope-OrgID"), r.URL.Query().Get("query"))
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"host-a"},"value":[1789387200.5,"1"]}]}}`)
	}))
	defer server.Close()

	client, err := New(Options{PrometheusURL: server.URL, LokiURL: server.URL, TempoURL: server.URL, PyroscopeURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	samples, err := client.PrometheusInstant(context.Background(), "tenant-a", "up", time.Unix(1789387200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].Labels["instance"] != "host-a" || samples[0].Value != 1 {
		t.Fatalf("unexpected samples: %#v", samples)
	}
}

func TestGatewayPathAndOrganizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prometheus/api/v1/query" || r.Header.Get("X-Sentinel-Organization") != "tenant-b" || r.Header.Get("X-Scope-OrgID") != "" {
			t.Fatalf("unexpected gateway request: path=%q headers=%v", r.URL.Path, r.Header)
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
	defer server.Close()

	client, err := New(Options{GatewayURL: server.URL, PrometheusURL: "unused", LokiURL: "unused", TempoURL: "unused", PyroscopeURL: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.PrometheusInstant(context.Background(), "tenant-b", "up", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestLokiRangeParsesStreams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"host_name":"host-a"},"values":[["1789387200000000000","service ready"]]}]}}`)
	}))
	defer server.Close()
	client, err := New(Options{PrometheusURL: server.URL, LokiURL: server.URL, TempoURL: server.URL, PyroscopeURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := client.LokiRange(context.Background(), "tenant-a", `{host_name="host-a"}`, time.Now().Add(-time.Minute), time.Now(), 20, "backward")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Line != "service ready" || entries[0].Labels["host_name"] != "host-a" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

func TestRetriesTransientBackendFailureOnce(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
	defer server.Close()
	client, err := New(Options{PrometheusURL: server.URL, LokiURL: server.URL, TempoURL: server.URL, PyroscopeURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.PrometheusInstant(context.Background(), "tenant-a", "up", time.Now()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected two attempts, got %d", attempts)
	}
}
