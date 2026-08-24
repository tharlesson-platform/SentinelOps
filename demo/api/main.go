package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var safeMarker = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

type appConfig struct {
	ServiceName   string
	Role          string
	Version       string
	Environment   string
	Team          string
	Owner         string
	Address       string
	DownstreamURL string
	TrafficTarget string
	TrafficEvery  time.Duration
}

type application struct {
	config           appConfig
	logger           *slog.Logger
	client           *http.Client
	registry         *prometheus.Registry
	requests         *prometheus.CounterVec
	duration         *prometheus.HistogramVec
	downstreamErrors *prometheus.CounterVec
}

func main() {
	config, err := loadConfig()
	if err != nil {
		slog.Error("demo configuration invalid", "error", err)
		os.Exit(1)
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	logger, shutdownTelemetry, err := telemetry(context.Background(), config)
	if err != nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
		logger.Warn("OTLP telemetry disabled", "error", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if shutdownErr := shutdownTelemetry(ctx); shutdownErr != nil {
				logger.Error("telemetry shutdown failed", "error", shutdownErr)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if config.TrafficTarget != "" {
		runTraffic(ctx, config, logger)
		return
	}
	if err := runServer(ctx, newApplication(config, logger)); err != nil {
		logger.Error("demo server stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig() (appConfig, error) {
	trafficEvery, err := time.ParseDuration(env("DEMO_TRAFFIC_INTERVAL", "2s"))
	if err != nil || trafficEvery < 250*time.Millisecond {
		return appConfig{}, fmt.Errorf("DEMO_TRAFFIC_INTERVAL must be a duration >=250ms")
	}
	config := appConfig{
		ServiceName:   env("SERVICE_NAME", "sentinel-demo-storefront"),
		Role:          env("SERVICE_ROLE", "storefront"),
		Version:       env("SERVICE_VERSION", "1.0.0"),
		Environment:   env("DEPLOYMENT_ENVIRONMENT", "local-demo"),
		Team:          env("SERVICE_TEAM", "platform"),
		Owner:         env("SERVICE_OWNER", "sentinelops"),
		Address:       env("HTTP_ADDR", ":8090"),
		DownstreamURL: os.Getenv("DEMO_DOWNSTREAM_URL"),
		TrafficTarget: os.Getenv("DEMO_TRAFFIC_TARGET"),
		TrafficEvery:  trafficEvery,
	}
	for name, value := range map[string]string{"SERVICE_NAME": config.ServiceName, "SERVICE_ROLE": config.Role, "SERVICE_VERSION": config.Version} {
		if !safeMarker.MatchString(value) {
			return appConfig{}, fmt.Errorf("%s contains unsupported characters", name)
		}
	}
	for name, value := range map[string]string{"DEMO_DOWNSTREAM_URL": config.DownstreamURL, "DEMO_TRAFFIC_TARGET": config.TrafficTarget} {
		if value == "" {
			continue
		}
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || parsed.Scheme != "http" || parsed.Hostname() == "" {
			return appConfig{}, fmt.Errorf("%s must be an absolute HTTP URL inside the demo network", name)
		}
	}
	return config, nil
}

func newApplication(config appConfig, logger *slog.Logger) *application {
	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "demo_pipeline_requests_total", Help: "Requests processed by the instrumented demo pipeline."}, []string{"service", "role", "route", "status", "version"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "demo_pipeline_request_duration_seconds", Help: "Request latency in the instrumented demo pipeline.", Buckets: prometheus.DefBuckets}, []string{"service", "role", "route", "version"})
	downstreamErrors := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "demo_pipeline_downstream_errors_total", Help: "Downstream failures observed by demo applications."}, []string{"service", "downstream"})
	registry.MustRegister(requests, duration, downstreamErrors)
	if config.Role == "linux-host" {
		registerLinuxHostMetrics(registry, config)
	}
	return &application{
		config:           config,
		logger:           logger,
		client:           &http.Client{Timeout: 5 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)},
		registry:         registry,
		requests:         requests,
		duration:         duration,
		downstreamErrors: downstreamErrors,
	}
}

func registerLinuxHostMetrics(registry *prometheus.Registry, config appConfig) {
	started := time.Now().Add(-6 * time.Hour)
	registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_uname_info", Help: "Mock Linux host identity.", ConstLabels: prometheus.Labels{"nodename": config.ServiceName, "release": "6.8.0-sentinel", "machine": "x86_64", "sysname": "Linux"}}, func() float64 { return 1 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_boot_time_seconds", Help: "Mock Linux host boot time."}, func() float64 { return float64(started.Unix()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_cpu_seconds_total", Help: "Mock cumulative idle CPU seconds.", ConstLabels: prometheus.Labels{"cpu": "0", "mode": "idle"}}, func() float64 { return float64(time.Now().UnixNano()) / 1e9 * 0.82 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_memory_MemTotal_bytes", Help: "Mock total memory."}, func() float64 { return 8 * 1024 * 1024 * 1024 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_memory_MemAvailable_bytes", Help: "Mock available memory."}, func() float64 { return 5 * 1024 * 1024 * 1024 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_filesystem_size_bytes", Help: "Mock filesystem size.", ConstLabels: prometheus.Labels{"device": "/dev/vda1", "fstype": "ext4", "mountpoint": "/"}}, func() float64 { return 80 * 1024 * 1024 * 1024 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_filesystem_avail_bytes", Help: "Mock filesystem available bytes.", ConstLabels: prometheus.Labels{"device": "/dev/vda1", "fstype": "ext4", "mountpoint": "/"}}, func() float64 { return 54 * 1024 * 1024 * 1024 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_filesystem_readonly", Help: "Mock filesystem readonly status.", ConstLabels: prometheus.Labels{"device": "/dev/vda1", "fstype": "ext4", "mountpoint": "/"}}, func() float64 { return 0 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_load1", Help: "Mock one minute load."}, func() float64 { return 0.42 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_processes_running", Help: "Mock running processes."}, func() float64 { return 7 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_network_receive_bytes_total", Help: "Mock received bytes.", ConstLabels: prometheus.Labels{"device": "eth0"}}, func() float64 { return float64(time.Now().UnixNano()) / 1e9 * 32768 }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "node_network_transmit_bytes_total", Help: "Mock transmitted bytes.", ConstLabels: prometheus.Labels{"device": "eth0"}}, func() float64 { return float64(time.Now().UnixNano()) / 1e9 * 16384 }),
	)
}

func (app *application) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(app.registry, promhttp.HandlerOpts{}))
	mux.Handle("GET /debug/pprof/", http.DefaultServeMux)
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"status": "ready", "service": app.config.ServiceName, "version": app.config.Version})
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { app.serve(w, r, "health", false) })
	mux.HandleFunc("GET /api/checkout", func(w http.ResponseWriter, r *http.Request) { app.serve(w, r, "checkout", true) })
	mux.HandleFunc("GET /api/orders", func(w http.ResponseWriter, r *http.Request) { app.serve(w, r, "orders", true) })
	mux.HandleFunc("GET /api/payments", func(w http.ResponseWriter, r *http.Request) { app.serve(w, r, "payments", true) })
	mux.HandleFunc("GET /api/queue", func(w http.ResponseWriter, r *http.Request) { app.serve(w, r, "queue", false) })
	return otelhttp.NewHandler(mux, app.config.ServiceName)
}

func runServer(ctx context.Context, app *application) error {
	server := &http.Server{Addr: app.config.Address, Handler: app.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	errorsChannel := make(chan error, 1)
	go func() {
		app.logger.Info("demo app started", "service", app.config.ServiceName, "role", app.config.Role, "version", app.config.Version, "address", app.config.Address, "downstream", app.config.DownstreamURL)
		errorsChannel <- server.ListenAndServe()
	}()
	select {
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func (app *application) serve(w http.ResponseWriter, r *http.Request, route string, callDownstream bool) {
	started := time.Now()
	span := trace.SpanFromContext(r.Context())
	marker := r.Header.Get("X-Demo-Marker")
	if !safeMarker.MatchString(marker) {
		marker = ""
	}
	synthetic := r.Header.Get("X-Synthetic-Test")
	span.SetAttributes(
		attribute.String("demo.service.role", app.config.Role),
		attribute.String("demo.route", route),
		attribute.String("demo.marker", marker),
		attribute.String("http.request.header.x_synthetic_test", synthetic),
	)

	status := http.StatusOK
	fault := requestedFault(r, app.config)
	switch fault {
	case "latency":
		time.Sleep(1500 * time.Millisecond)
	case "timeout":
		time.Sleep(6 * time.Second)
	case "error":
		status = http.StatusInternalServerError
	case "cpu":
		for index := 0; index < 4_000_000; index++ {
			_ = math.Sqrt(float64(index))
		}
	case "exception":
		status = http.StatusInternalServerError
		app.logger.ErrorContext(r.Context(), "controlled demo exception", "service", app.config.ServiceName, "route", route, "trace_id", otelTraceID(r), "marker", marker)
	}

	var downstream any
	if status < http.StatusBadRequest && callDownstream && app.config.DownstreamURL != "" {
		response, err := app.callDownstream(r)
		if err != nil {
			status = http.StatusBadGateway
			app.downstreamErrors.WithLabelValues(app.config.ServiceName, app.config.DownstreamURL).Inc()
			span.RecordError(err)
			downstream = map[string]any{"error": err.Error()}
		} else {
			defer response.Body.Close()
			if decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&downstream); decodeErr != nil {
				status = http.StatusBadGateway
				span.RecordError(decodeErr)
				downstream = map[string]any{"error": "invalid downstream response"}
			} else if response.StatusCode >= http.StatusBadRequest {
				status = http.StatusBadGateway
				app.downstreamErrors.WithLabelValues(app.config.ServiceName, app.config.DownstreamURL).Inc()
			}
		}
	}

	duration := time.Since(started)
	app.requests.WithLabelValues(app.config.ServiceName, app.config.Role, route, strconv.Itoa(status), app.config.Version).Inc()
	app.duration.WithLabelValues(app.config.ServiceName, app.config.Role, route, app.config.Version).Observe(duration.Seconds())
	span.SetAttributes(attribute.Int("http.response.status_code", status), attribute.Int64("demo.duration_ms", duration.Milliseconds()))
	if status >= http.StatusBadRequest {
		span.SetStatus(codes.Error, http.StatusText(status))
	}
	app.logger.InfoContext(r.Context(), "demo_request", "service", app.config.ServiceName, "role", app.config.Role, "route", route, "status", status, "duration_ms", duration.Milliseconds(), "service.version", app.config.Version, "trace_id", otelTraceID(r), "marker", marker, "synthetic", synthetic != "", "fault", fault)
	w.Header().Set("X-Trace-ID", otelTraceID(r))
	jsonResponse(w, status, map[string]any{
		"status":     map[bool]string{true: "ok", false: "error"}[status < http.StatusBadRequest],
		"service":    app.config.ServiceName,
		"role":       app.config.Role,
		"route":      route,
		"version":    app.config.Version,
		"traceId":    otelTraceID(r),
		"marker":     marker,
		"downstream": downstream,
	})
}

func requestedFault(r *http.Request, config appConfig) string {
	fault := r.URL.Query().Get("fault")
	if fault == "" {
		fault = r.Header.Get("X-Demo-Fault")
	}
	if fault == "" {
		fault = os.Getenv("DEMO_FAULT")
	}
	target := r.URL.Query().Get("fault_service")
	if target != "" && target != config.Role && target != config.ServiceName {
		return ""
	}
	return fault
}

func (app *application) callDownstream(r *http.Request) (*http.Response, error) {
	target, err := url.Parse(app.config.DownstreamURL)
	if err != nil {
		return nil, err
	}
	target.RawQuery = r.URL.Query().Encode()
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	for _, header := range []string{"X-Demo-Marker", "X-Synthetic-Test", "X-Demo-Fault"} {
		if value := r.Header.Get(header); value != "" {
			request.Header.Set(header, value)
		}
	}
	return app.client.Do(request)
}

func runTraffic(ctx context.Context, config appConfig, logger *slog.Logger) {
	client := &http.Client{Timeout: 8 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}
	ticker := time.NewTicker(config.TrafficEvery)
	defer ticker.Stop()
	sequence := 0
	logger.Info("demo traffic generator started", "target", config.TrafficTarget, "interval", config.TrafficEvery)
	for {
		sequence++
		emitTraffic(ctx, client, config, logger, sequence)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func emitTraffic(parent context.Context, client *http.Client, config appConfig, logger *slog.Logger, sequence int) {
	target, err := url.Parse(config.TrafficTarget)
	if err != nil {
		logger.Error("traffic target invalid", "error", err)
		return
	}
	query := target.Query()
	if sequence%20 == 0 {
		query.Set("fault", "error")
		query.Set("fault_service", "payments")
	} else if sequence%12 == 0 {
		query.Set("fault", "latency")
		query.Set("fault_service", "orders")
	}
	target.RawQuery = query.Encode()
	ctx, span := otel.Tracer(config.ServiceName).Start(parent, "generate-demo-traffic")
	defer span.End()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		span.RecordError(err)
		return
	}
	marker := fmt.Sprintf("continuous-%06d", sequence)
	request.Header.Set("X-Demo-Marker", marker)
	request.Header.Set("X-Synthetic-Test", "sentinelops")
	response, err := client.Do(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.WarnContext(ctx, "demo traffic failed", "error", err, "marker", marker)
		return
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode), attribute.String("demo.marker", marker))
	if response.StatusCode >= http.StatusBadRequest {
		span.SetStatus(codes.Error, response.Status)
	}
	logger.InfoContext(ctx, "demo traffic completed", "status", response.StatusCode, "marker", marker, "trace_id", span.SpanContext().TraceID().String())
}

func telemetry(ctx context.Context, config appConfig) (*slog.Logger, func(context.Context) error, error) {
	endpoint := env("OTEL_EXPORTER_OTLP_ENDPOINT", "http://alloy:4318")
	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"))
	if err != nil {
		return nil, nil, err
	}
	resourceAttributes, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", config.ServiceName),
		attribute.String("service.namespace", "sentinel-demo"),
		attribute.String("service.version", config.Version),
		attribute.String("deployment.environment.name", config.Environment),
		attribute.String("team", config.Team),
		attribute.String("owner", config.Owner),
		attribute.String("demo.role", config.Role),
	))
	if err != nil {
		return nil, nil, err
	}
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(resourceAttributes), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	logExporter, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(endpoint+"/v1/logs"))
	if err != nil {
		return nil, nil, err
	}
	logProvider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)), sdklog.WithResource(resourceAttributes))
	otel.SetTracerProvider(traceProvider)
	logger := slog.New(&teeHandler{handlers: []slog.Handler{slog.NewJSONHandler(os.Stdout, nil), otelslog.NewHandler(config.ServiceName, otelslog.WithLoggerProvider(logProvider))}})
	shutdown := func(ctx context.Context) error {
		return errors.Join(traceProvider.Shutdown(ctx), logProvider.Shutdown(ctx))
	}
	return logger, shutdown, nil
}

func otelTraceID(r *http.Request) string {
	return trace.SpanContextFromContext(r.Context()).TraceID().String()
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

type teeHandler struct{ handlers []slog.Handler }

func (handler *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, child := range handler.handlers {
		if child.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (handler *teeHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, child := range handler.handlers {
		if child.Enabled(ctx, record.Level) {
			if err := child.Handle(ctx, record.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (handler *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(handler.handlers))
	for index, child := range handler.handlers {
		next[index] = child.WithAttrs(attrs)
	}
	return &teeHandler{handlers: next}
}

func (handler *teeHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(handler.handlers))
	for index, child := range handler.handlers {
		next[index] = child.WithGroup(name)
	}
	return &teeHandler{handlers: next}
}
