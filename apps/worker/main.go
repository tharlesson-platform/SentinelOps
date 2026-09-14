package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sentinelops/sentinelops/internal/config"
	"github.com/sentinelops/sentinelops/internal/database"
	"github.com/sentinelops/sentinelops/internal/events"
	"github.com/sentinelops/sentinelops/internal/synthetics"
	"github.com/sentinelops/sentinelops/internal/workflows"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "error", err)
		os.Exit(1)
	}
	if err := cfg.ValidateWorker(); err != nil {
		logger.Error("worker configuration invalid", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	tc, err := client.Dial(client.Options{HostPort: cfg.TemporalAddress, Namespace: cfg.TemporalNamespace})
	if err != nil {
		logger.Error("temporal unavailable", "error", err)
		os.Exit(1)
	}
	defer tc.Close()
	w := worker.New(tc, workflows.ReleaseValidationTaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.ReleaseValidationWorkflow)
	validationHosts := splitList(os.Getenv("RELEASE_VALIDATION_ALLOWED_HOSTS"))
	telemetryClient, telemetryURLs, err := telemetryQueryClient(cfg)
	if err != nil {
		logger.Error("telemetry query client invalid", "error", err)
		os.Exit(1)
	}
	w.RegisterActivity(&workflows.Activities{Store: store, HTTPClient: telemetryClient, ReleaseValidationBaseURL: os.Getenv("RELEASE_VALIDATION_BASE_URL"), AllowedHealthHosts: validationHosts,
		PrometheusURL: telemetryURLs.prometheus, LokiURL: telemetryURLs.loki, TempoURL: telemetryURLs.tempo})
	var ready atomic.Bool
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	healthMux.HandleFunc("GET /readyz", func(response http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	healthServer := &http.Server{Addr: env("WORKER_HTTP_ADDR", ":8081"), Handler: healthMux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if listenErr := healthServer.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			logger.Error("worker health server stopped", "error", listenErr)
			os.Exit(1)
		}
	}()
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = healthServer.Shutdown(shutdownCtx)
	}()
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	allowedTargets, err := synthetics.ParseAllowedTargets(os.Getenv("SYNTHETIC_ALLOWED_TARGETS"))
	if err != nil {
		logger.Error("synthetic target policy invalid", "error", err)
		os.Exit(1)
	}
	go (&synthetics.Scheduler{Store: store, Client: &http.Client{Timeout: 12 * time.Second}, Logger: logger, Interval: time.Minute, AllowedTargets: allowedTargets}).Run(schedulerCtx)
	go (&events.Dispatcher{Store: store, Logger: logger, Interval: 2 * time.Second, HTTPClient: &http.Client{Timeout: 10 * time.Second}, WebhookURLs: cfg.NotificationWebhookURLs, AllowedHosts: cfg.NotificationAllowedHosts}).Run(schedulerCtx)
	ready.Store(true)
	logger.Info("worker started", "queue", workflows.ReleaseValidationTaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func splitList(value string) []string {
	items := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

type telemetryURLs struct {
	prometheus string
	loki       string
	tempo      string
}

func telemetryQueryClient(cfg config.Config) (*http.Client, telemetryURLs, error) {
	urls := telemetryURLs{
		prometheus: env("PROMETHEUS_URL", "http://prometheus:9090"),
		loki:       env("LOKI_URL", "http://loki:3100"),
		tempo:      env("TEMPO_URL", "http://tempo:3200"),
	}
	if cfg.TelemetryQueryGatewayURL == "" {
		return &http.Client{Timeout: 12 * time.Second}, urls, nil
	}
	base, err := url.Parse(cfg.TelemetryQueryGatewayURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil {
		return nil, telemetryURLs{}, errors.New("TELEMETRY_QUERY_GATEWAY_URL must be an absolute HTTPS URL")
	}
	certificate, err := tls.LoadX509KeyPair(cfg.TelemetryQueryClientCert, cfg.TelemetryQueryClientKey)
	if err != nil {
		return nil, telemetryURLs{}, fmt.Errorf("load telemetry query mTLS certificate: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/")
	urls = telemetryURLs{
		prometheus: base.String() + "/prometheus",
		loki:       base.String() + "/loki",
		tempo:      base.String() + "/tempo",
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	return &http.Client{Timeout: 12 * time.Second, Transport: transport}, urls, nil
}
