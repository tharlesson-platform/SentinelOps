package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestDemoPipelinePropagatesTraceMarkerAndQuery(t *testing.T) {
	originalProvider := otel.GetTracerProvider()
	originalPropagator := otel.GetTextMapPropagator()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(originalProvider)
		otel.SetTextMapPropagator(originalPropagator)
	})

	downstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if traceparent := request.Header.Get("traceparent"); !regexp.MustCompile(`^00-[a-f0-9]{32}-[a-f0-9]{16}-01$`).MatchString(traceparent) {
			t.Errorf("trace context was not propagated: %q", traceparent)
		}
		if marker := request.Header.Get("X-Demo-Marker"); marker != "proof-marker" {
			t.Errorf("marker was not propagated: %q", marker)
		}
		if got := request.URL.Query().Get("fault_service"); got != "payments" {
			t.Errorf("fault target was not propagated: %q", got)
		}
		jsonResponse(response, http.StatusOK, map[string]any{"service": "sentinel-demo-payments"})
	}))
	t.Cleanup(downstream.Close)

	config := appConfig{ServiceName: "sentinel-demo-storefront", Role: "storefront", Version: "1.0.0", DownstreamURL: downstream.URL, TrafficEvery: time.Second}
	app := newApplication(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/api/checkout?fault=error&fault_service=payments", nil)
	request.Header.Set("X-Demo-Marker", "proof-marker")
	request.Header.Set("X-Synthetic-Test", "sentinelops")
	response := httptest.NewRecorder()
	app.handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		TraceID    string         `json:"traceId"`
		Marker     string         `json:"marker"`
		Downstream map[string]any `json:"downstream"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(body.TraceID) {
		t.Fatalf("invalid trace ID: %q", body.TraceID)
	}
	if body.Marker != "proof-marker" || body.Downstream["service"] != "sentinel-demo-payments" {
		t.Fatalf("unexpected response: %#v", body)
	}
}

func TestFaultTargetsOnlySelectedService(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/orders?fault=error&fault_service=payments", nil)
	if got := requestedFault(request, appConfig{ServiceName: "sentinel-demo-orders", Role: "orders"}); got != "" {
		t.Fatalf("orders service applied payment fault: %q", got)
	}
	if got := requestedFault(request, appConfig{ServiceName: "sentinel-demo-payments", Role: "payments"}); got != "error" {
		t.Fatalf("payments service did not apply targeted fault: %q", got)
	}
}

func TestLoadConfigRejectsExternalSchemesAndFastTraffic(t *testing.T) {
	t.Setenv("DEMO_DOWNSTREAM_URL", "https://example.com")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected HTTPS downstream to be rejected in the isolated demo network")
	}
	t.Setenv("DEMO_DOWNSTREAM_URL", "")
	t.Setenv("DEMO_TRAFFIC_INTERVAL", "10ms")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected unsafe traffic interval to be rejected")
	}
}
