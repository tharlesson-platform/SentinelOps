package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "demo_http_requests_total", Help: "Demo requests."}, []string{"route", "status", "version"})
var duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "demo_http_request_duration_seconds", Help: "Demo latency.", Buckets: prometheus.DefBuckets}, []string{"route", "version"})

func main() {
	version := env("SERVICE_VERSION", "1.0.0")
	prometheus.MustRegister(requests, duration)
	logger, shutdown, err := telemetry(context.Background(), version)
	if err != nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
		logger.Warn("OTLP telemetry disabled", "error", err)
	} else {
		defer shutdown(context.Background())
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("GET /debug/pprof/", http.DefaultServeMux)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { serve(w, r, logger, version, "health") })
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{"status": "ready", "version": version})
	})
	mux.HandleFunc("GET /api/orders", func(w http.ResponseWriter, r *http.Request) { serve(w, r, logger, version, "orders") })
	mux.HandleFunc("GET /api/queue", func(w http.ResponseWriter, r *http.Request) { serve(w, r, logger, version, "queue") })
	server := &http.Server{Addr: ":8090", Handler: otelhttp.NewHandler(mux, "demo-api"), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	logger.Info("demo api started", "version", version, "address", ":8090")
	if err := server.ListenAndServe(); err != nil {
		logger.Error("server stopped", "error", err)
	}
}
func serve(w http.ResponseWriter, r *http.Request, logger *slog.Logger, version, route string) {
	start := time.Now()
	fault := r.URL.Query().Get("fault")
	if fault == "" {
		fault = r.Header.Get("X-Demo-Fault")
	}
	if fault == "" {
		fault = os.Getenv("DEMO_FAULT")
	}
	status := 200
	switch fault {
	case "latency":
		time.Sleep(2 * time.Second)
	case "timeout":
		time.Sleep(16 * time.Second)
	case "error":
		status = 500
	case "cpu":
		for i := 0; i < 2_000_000; i++ {
			_ = math.Sqrt(float64(i))
		}
	case "exception":
		logger.Error("controlled demo exception", "route", route, "trace_id", otelTraceID(r))
		status = 500
	}
	requests.WithLabelValues(route, strconv.Itoa(status), version).Inc()
	duration.WithLabelValues(route, version).Observe(time.Since(start).Seconds())
	logger.Info("demo_request", "route", route, "status", status, "duration_ms", time.Since(start).Milliseconds(), "service.version", version, "trace_id", otelTraceID(r), "synthetic", r.Header.Get("X-Synthetic-Test") != "")
	jsonResponse(w, status, map[string]any{"status": map[bool]string{true: "ok", false: "error"}[status < 400], "route": route, "version": version, "traceId": otelTraceID(r), "orders": []map[string]any{{"id": "demo-1", "total": 42.5}}})
}
func telemetry(ctx context.Context, version string) (*slog.Logger, func(context.Context) error, error) {
	endpoint := env("OTEL_EXPORTER_OTLP_ENDPOINT", "http://alloy:4318")
	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"))
	if err != nil {
		return nil, nil, err
	}
	res, err := resource.New(ctx, resource.WithAttributes(attribute.String("service.name", "sentinel-demo-api"), attribute.String("service.version", version), attribute.String("deployment.environment.name", "local-demo"), attribute.String("team", "platform")))
	if err != nil {
		return nil, nil, err
	}
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(res))
	logExporter, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(endpoint+"/v1/logs"))
	if err != nil {
		return nil, nil, err
	}
	logProvider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)), sdklog.WithResource(res))
	otel.SetTracerProvider(traceProvider)
	logger := slog.New(&teeHandler{handlers: []slog.Handler{slog.NewJSONHandler(os.Stdout, nil), otelslog.NewHandler("sentinel-demo-api", otelslog.WithLoggerProvider(logProvider))}})
	shutdown := func(ctx context.Context) error {
		traceErr := traceProvider.Shutdown(ctx)
		logErr := logProvider.Shutdown(ctx)
		if traceErr != nil {
			return traceErr
		}
		return logErr
	}
	return logger, shutdown, nil
}
func otelTraceID(r *http.Request) string {
	return trace.SpanContextFromContext(r.Context()).TraceID().String()
}

type headerCarrier struct{ http.Header }

func (h headerCarrier) Get(k string) string { return h.Header.Get(k) }
func (h headerCarrier) Set(k, v string)     { h.Header.Set(k, v) }
func (h headerCarrier) Keys() []string {
	keys := make([]string, 0, len(h.Header))
	for k := range h.Header {
		keys = append(keys, k)
	}
	return keys
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func env(k, v string) string {
	if x := os.Getenv(k); x != "" {
		return x
	}
	return v
}

var _ = fmt.Sprintf

type teeHandler struct{ handlers []slog.Handler }

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, x := range h.handlers {
		if x.Enabled(ctx, level) {
			return true
		}
	}
	return false
}
func (h *teeHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, x := range h.handlers {
		if x.Enabled(ctx, record.Level) {
			if err := x.Handle(ctx, record.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}
func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, x := range h.handlers {
		next[i] = x.WithAttrs(attrs)
	}
	return &teeHandler{handlers: next}
}
func (h *teeHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, x := range h.handlers {
		next[i] = x.WithGroup(name)
	}
	return &teeHandler{handlers: next}
}
