package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/sentinelops/sentinelops/internal/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	databaseURL := os.Getenv("DATABASE_MIGRATION_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_MIGRATION_URL is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	store, err := database.Open(ctx, databaseURL)
	if err != nil {
		logger.Error("migration database unavailable", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}
	logger.Info("database migration completed")
}
