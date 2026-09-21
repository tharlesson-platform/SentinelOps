package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sentinelops/sentinelops/internal/telemetryquery"
)

func TestAPMUndefinedQuantilesProduceValidJSONWithMissingValues(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := "1"
		query := r.URL.Query().Get("query")
		if strings.Contains(query, "histogram_quantile(0.95") {
			value = "NaN"
		}
		if strings.Contains(query, "histogram_quantile(0.99") {
			value = "+Inf"
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"service_name":"api","host_name":"node","deployment_environment":"prod"},"value":[1000,%q]}]}}`, value)
	}))
	defer backend.Close()
	client, err := telemetryquery.New(telemetryquery.Options{GatewayURL: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	s := Server{telemetry: client}
	results := s.instantQueries(context.Background(), "tenant", map[string]string{"rps": "rate(count[5m])", "p95": "histogram_quantile(0.95, fixture)", "p99": "histogram_quantile(0.99, fixture)"})
	items := buildAPMServices(results)
	if len(items) != 1 || items[0].RequestsPerS == nil || *items[0].RequestsPerS != 1 || items[0].P95Seconds != nil || items[0].P99Seconds != nil {
		t.Fatalf("invalid missing-value contract: %#v", items)
	}
	w := httptest.NewRecorder()
	write(w, http.StatusOK, map[string]any{"items": items, "sources": statusFromInstantResults(results)})
	if w.Code != 200 || !json.Valid(w.Body.Bytes()) || !strings.Contains(w.Body.String(), `"p95Seconds":null`) {
		t.Fatalf("APM must return valid JSON with null quantiles: status=%d", w.Code)
	}
	if statusFromInstantResults(map[string]instantResult{"p95": results["p95"]}).State != "no_data" {
		t.Fatal("undefined-only metrics must report no_data")
	}
}

func TestObservedIdentityDoesNotGuessExporterEquivalence(t *testing.T) {
	hosts := buildHosts(map[string]instantResult{"identity": {samples: []telemetryquery.VectorSample{{Labels: map[string]string{"instance": "10.0.0.1:9100", "nodename": "node-a"}}}}})
	if len(hosts) != 1 || hosts[0].HostName != "" {
		t.Fatal("instance/nodename must not be guessed as a cross-source identity")
	}
	hosts = buildHosts(map[string]instantResult{"identity": {samples: []telemetryquery.VectorSample{{Labels: map[string]string{"instance": "10.0.0.1:9100", "nodename": "node-a", "host_name": "canonical-a"}}}}})
	if hosts[0].HostName != "canonical-a" || hosts[0].Instance != "10.0.0.1:9100" {
		t.Fatal("canonical identity must not replace metric selector")
	}
}
func TestObservedContainersWithSameNameRemainDistinct(t *testing.T) {
	samples := []telemetryquery.VectorSample{}
	for _, host := range []string{"a:8080", "b:8080"} {
		samples = append(samples, telemetryquery.VectorSample{Labels: map[string]string{"instance": host, "name": "api", "host_name": "canonical-" + host, "id": "/docker/" + strings.Repeat("a", 64)}, Timestamp: time.Now(), Value: float64(time.Now().Unix())})
	}
	items := buildContainers(map[string]instantResult{"identity": {samples: samples}})
	if len(items) != 2 || items[0].Host == items[1].Host {
		t.Fatal("same container name on distinct hosts was merged")
	}
	if items[0].LogContainerID != strings.Repeat("a", 64) || items[0].HostName != "canonical-a:8080" {
		t.Fatal("explicit correlation was lost")
	}
}
func TestDockerLogIdentityOnlyAcceptsFullKnownCgroupIDs(t *testing.T) {
	id := strings.Repeat("f", 64)
	for _, valid := range []string{id, "/docker/" + id, "/system.slice/docker-" + id + ".scope"} {
		if dockerLogID(valid) != id {
			t.Errorf("expected full ID for %s", valid)
		}
	}
	for _, invalid := range []string{"nginx", "/docker/abcdefabcdef", "prefix" + id, "/unknown/" + id} {
		if dockerLogID(invalid) != "" {
			t.Errorf("guessed identity from %s", invalid)
		}
	}
}
func TestAPMSameServiceAcrossEnvironmentsAndHosts(t *testing.T) {
	results := map[string]instantResult{}
	for _, metric := range []string{"rps", "errors", "p95", "p99"} {
		results[metric] = instantResult{samples: []telemetryquery.VectorSample{{Labels: map[string]string{"service_name": "api", "deployment_environment": "prod", "host_name": "a"}, Value: 10}, {Labels: map[string]string{"service_name": "api", "deployment_environment": "dev", "host_name": "b"}, Value: 20}}}
	}
	items := buildAPMServices(results)
	if len(items) != 2 {
		t.Fatal("APM merged distinct environments")
	}
	for _, item := range items {
		expected := 10.0
		if item.Host == "b" {
			expected = 20
		}
		if *item.ErrorPercent != expected || *item.P95Seconds != expected {
			t.Fatal("RED metrics mixed identities")
		}
	}
}

func TestGlobalLogsIncludeHostsAndServicesWithBoundedSortedResults(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		seen = append(seen, query)
		if r.URL.Query().Get("limit") != "2" {
			t.Errorf("unbounded request")
		}
		if strings.HasPrefix(query, `{host_name=`) {
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"host_name":"node-a"},"values":[["3000000000","host-new"],["1000000000","host-old"]]}]}}`)
		} else {
			fmt.Fprint(w, `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"service_name":"api"},"values":[["2000000000","service"]]}]}}`)
		}
	}))
	defer server.Close()
	client, err := telemetryquery.New(telemetryquery.Options{GatewayURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	s := Server{telemetry: client}
	entries, queries, status := s.queryObservedLogs(context.Background(), "fixture", `{service_name=~".+"} |= "error"`, true, time.Unix(0, 0), time.Unix(10, 0), 2)
	if len(seen) != 2 || queries[1] != `{host_name=~".+",service_name=""} |= "error"` {
		t.Fatalf("missing host selector: %v", queries)
	}
	if len(entries) != 2 || entries[0].Line != "host-new" || entries[1].Line != "service" || status.State != "available" {
		t.Fatalf("global order/limit failed: %v %v", entries, status)
	}
}
func TestGlobalLogsPreservePartialAndScopedQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Query().Get("query"), `{host_name=`) {
			http.Error(w, "fixture unavailable", 503)
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"streams","result":[]}}`)
	}))
	defer server.Close()
	client, err := telemetryquery.New(telemetryquery.Options{GatewayURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	s := Server{telemetry: client}
	_, _, status := s.queryObservedLogs(context.Background(), "fixture", `{service_name=~".+"}`, true, time.Unix(0, 0), time.Unix(10, 0), 2)
	if status.State != "partial" {
		t.Fatalf("failure hidden: %v", status)
	}
	if queries := observedLogQueries(`{host_name="node-a",container_id="id"}`, false); len(queries) != 1 {
		t.Fatal("scoped request expanded globally")
	}
}
