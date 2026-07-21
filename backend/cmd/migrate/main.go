package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/migration"
	"github.com/GulovM/PharmacyCRM/backend/migrations"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logger, err := logging.New(cfg.Logging, cfg.App)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "logger shutdown failed")
		}
	}()
	pool, err := database.NewMigration(context.Background(), cfg.Postgres)
	if err != nil {
		fmt.Fprintln(os.Stderr, "initialize migration pool")
		os.Exit(1)
	}
	defer pool.Close()
	items, err := migration.Load(migrations.Files)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load migrations")
		os.Exit(1)
	}
	result, err := migration.Run(context.Background(), pool, items)
	if err != nil {
		logger.Error("migration.failed", zap.Error(err))
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "failed"})
		os.Exit(1)
	}
	logger.Info("migration.completed", zap.Int("applied", len(result.Applied)), zap.Int64("schema_version", result.SchemaVersion))
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
