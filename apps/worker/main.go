package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
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
	w.RegisterActivity(&workflows.Activities{Store: store, HTTPClient: &http.Client{Timeout: 12 * time.Second}, DemoBaseURL: os.Getenv("DEMO_BASE_URL")})
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	allowedHosts := strings.Split(env("SYNTHETIC_ALLOWED_HOSTS", "demo-api"), ",")
	go (&synthetics.Scheduler{Store: store, Client: &http.Client{Timeout: 12 * time.Second}, Logger: logger, Interval: time.Minute, AllowedHosts: allowedHosts}).Run(schedulerCtx)
	logger.Info("worker started", "queue", workflows.ReleaseValidationTaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
