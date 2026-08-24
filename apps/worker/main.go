package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sentinelops/sentinelops/internal/config"
	"github.com/sentinelops/sentinelops/internal/database"
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
	validationHosts := splitList(env("RELEASE_VALIDATION_ALLOWED_HOSTS", "demo-api"))
	w.RegisterActivity(&workflows.Activities{Store: store, HTTPClient: &http.Client{Timeout: 12 * time.Second}, DemoBaseURL: os.Getenv("DEMO_BASE_URL"), AllowedHealthHosts: validationHosts,
		PrometheusURL: env("PROMETHEUS_URL", "http://prometheus:9090"), LokiURL: env("LOKI_URL", "http://loki:3100"), TempoURL: env("TEMPO_URL", "http://tempo:3200")})
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
	allowedHosts := splitList(env("SYNTHETIC_ALLOWED_HOSTS", "demo-api"))
	go (&synthetics.Scheduler{Store: store, Client: &http.Client{Timeout: 12 * time.Second}, Logger: logger, Interval: time.Minute, AllowedHosts: allowedHosts}).Run(schedulerCtx)
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
