// postgresexporter exposes a small, read-only PostgreSQL health surface.
// It intentionally never queries query text, database content, roles or
// credentials, and uses only fixed aggregate SELECT statements.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const aggregateQuery = `SELECT
  (SELECT count(*) FROM pg_stat_activity),
  (SELECT count(*) FROM pg_stat_activity WHERE state = 'active'),
  (SELECT count(*) FROM pg_stat_activity WHERE wait_event_type IS NOT NULL),
  (SELECT count(*) FROM pg_locks WHERE NOT granted),
  (SELECT setting::float8 FROM pg_settings WHERE name = 'max_connections'),
  (SELECT coalesce(sum(deadlocks), 0) FROM pg_stat_database)`

type config struct {
	dsnFile, address           string
	assetID, team, environment string
	interval                   time.Duration
}

var safeLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := load()
	if err != nil {
		logger.Error("PostgreSQL exporter configuration invalid", "error", err)
		os.Exit(1)
	}
	registry := prometheus.NewRegistry()
	exporter := newExporter(cfg, logger, registry)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	exporter.refresh(ctx)
	go exporter.loop(ctx)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /-/ready", exporter.ready)
	server := &http.Server{Addr: cfg.address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = server.Shutdown(context.Background()) }()
	logger.Info("PostgreSQL exporter listening", "address", cfg.address, "interval", cfg.interval)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("PostgreSQL exporter stopped", "error", err)
		os.Exit(1)
	}
}

func load() (config, error) {
	cfg := config{dsnFile: os.Getenv("POSTGRES_MONITOR_DSN_FILE"), address: env("POSTGRES_EXPORTER_ADDRESS", "127.0.0.1:9187"), assetID: os.Getenv("POSTGRES_MONITOR_ASSET_ID"), team: os.Getenv("POSTGRES_MONITOR_TEAM"), environment: os.Getenv("POSTGRES_MONITOR_ENVIRONMENT"), interval: 30 * time.Second}
	if raw := os.Getenv("POSTGRES_SCRAPE_INTERVAL"); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil {
			return cfg, fmt.Errorf("POSTGRES_SCRAPE_INTERVAL: %w", err)
		}
		cfg.interval = interval
	}
	if cfg.dsnFile == "" {
		return cfg, errors.New("POSTGRES_MONITOR_DSN_FILE é obrigatório")
	}
	if !safeLabel.MatchString(cfg.assetID) || !safeLabel.MatchString(cfg.team) || !safeLabel.MatchString(cfg.environment) {
		return cfg, errors.New("POSTGRES_MONITOR_ASSET_ID, POSTGRES_MONITOR_TEAM e POSTGRES_MONITOR_ENVIRONMENT devem ser DNS-safe")
	}
	if cfg.interval < 15*time.Second || cfg.interval > 10*time.Minute {
		return cfg, errors.New("POSTGRES_SCRAPE_INTERVAL deve estar entre 15s e 10m")
	}
	host, _, err := net.SplitHostPort(cfg.address)
	if err != nil {
		return cfg, fmt.Errorf("POSTGRES_EXPORTER_ADDRESS: %w", err)
	}
	if host != "localhost" && !net.ParseIP(host).IsLoopback() && os.Getenv("POSTGRES_ALLOW_CONTAINER_BIND") != "true" {
		return cfg, errors.New("POSTGRES_EXPORTER_ADDRESS deve usar loopback")
	}
	if _, err := readDSN(cfg.dsnFile); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func readDSN(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat POSTGRES_MONITOR_DSN_FILE: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("POSTGRES_MONITOR_DSN_FILE não pode conceder permissões a grupo ou outros")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read POSTGRES_MONITOR_DSN_FILE: %w", err)
	}
	dsn := strings.TrimSpace(string(data))
	if dsn == "" {
		return "", errors.New("POSTGRES_MONITOR_DSN_FILE está vazio")
	}
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return "", fmt.Errorf("DSN PostgreSQL inválido: %w", err)
	}
	if poolCfg.ConnConfig.TLSConfig == nil || poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		return "", errors.New("DSN PostgreSQL deve usar TLS com validação de certificado")
	}
	return dsn, nil
}

type exporter struct {
	cfg                                                                             config
	logger                                                                          *slog.Logger
	mu                                                                              sync.RWMutex
	lastOK                                                                          time.Time
	success, lastSuccess, connections, active, waiting, blocked, maximum, deadlocks prometheus.Gauge
}

func newExporter(cfg config, logger *slog.Logger, registry *prometheus.Registry) *exporter {
	e := &exporter{cfg: cfg, logger: logger,
		success:     labeledGauge("sentinelops_postgres_scrape_success", "1 when the latest PostgreSQL aggregate collection succeeded.", cfg),
		lastSuccess: labeledGauge("sentinelops_postgres_last_success_unixtime", "Unix timestamp of the latest successful PostgreSQL aggregate collection.", cfg),
		connections: labeledGauge("sentinelops_postgres_connections", "Total open PostgreSQL connections.", cfg),
		active:      labeledGauge("sentinelops_postgres_active_connections", "Active PostgreSQL connections.", cfg),
		waiting:     labeledGauge("sentinelops_postgres_waiting_connections", "Connections with a PostgreSQL wait event.", cfg),
		blocked:     labeledGauge("sentinelops_postgres_blocked_locks", "PostgreSQL locks not currently granted.", cfg),
		maximum:     labeledGauge("sentinelops_postgres_max_connections", "Configured PostgreSQL maximum connections.", cfg),
		deadlocks:   labeledGauge("sentinelops_postgres_deadlocks_total", "Aggregate PostgreSQL deadlocks since statistics reset.", cfg),
	}
	registry.MustRegister(e.success, e.lastSuccess, e.connections, e.active, e.waiting, e.blocked, e.maximum, e.deadlocks)
	return e
}

func labeledGauge(name, help string, cfg config) prometheus.Gauge {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, []string{"asset_id", "team", "environment"}).WithLabelValues(cfg.assetID, cfg.team, cfg.environment)
}

func (e *exporter) loop(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.refresh(ctx)
		}
	}
}

func (e *exporter) refresh(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	dsn, err := readDSN(e.cfg.dsnFile)
	if err != nil {
		e.fail(err)
		return
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		e.fail(fmt.Errorf("connect PostgreSQL: %w", err))
		return
	}
	defer pool.Close()
	var connections, active, waiting, blocked, maximum, deadlocks float64
	err = pool.QueryRow(ctx, aggregateQuery).Scan(&connections, &active, &waiting, &blocked, &maximum, &deadlocks)
	if err != nil {
		e.fail(fmt.Errorf("read PostgreSQL aggregate stats: %w", err))
		return
	}
	e.connections.Set(connections)
	e.active.Set(active)
	e.waiting.Set(waiting)
	e.blocked.Set(blocked)
	e.maximum.Set(maximum)
	e.deadlocks.Set(deadlocks)
	now := time.Now().UTC()
	e.mu.Lock()
	e.lastOK = now
	e.mu.Unlock()
	e.success.Set(1)
	e.lastSuccess.Set(float64(now.Unix()))
}

func (e *exporter) fail(err error) {
	e.success.Set(0)
	// No DSN or server response is logged: both may contain sensitive details.
	e.logger.Warn("PostgreSQL aggregate collection failed", "class", errorClass(err))
}

func errorClass(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "permission denied"), strings.Contains(message, "insufficient privilege"):
		return "access_denied"
	case strings.Contains(message, "deadline"), strings.Contains(message, "timeout"):
		return "timeout"
	case strings.Contains(message, "certificate"), strings.Contains(message, "tls"):
		return "tls_failure"
	default:
		return "collection_failed"
	}
}

func (e *exporter) ready(w http.ResponseWriter, _ *http.Request) {
	e.mu.RLock()
	lastOK := e.lastOK
	e.mu.RUnlock()
	if lastOK.IsZero() || time.Since(lastOK) > e.cfg.interval*2 {
		http.Error(w, "PostgreSQL collection is stale", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
