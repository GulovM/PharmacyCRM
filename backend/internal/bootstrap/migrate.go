package bootstrap

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

func RunMigration() error {
	cfg, err := config.LoadMigration()
	if err != nil {
		return err
	}
	logger, err := logging.New(cfg.Logging, cfg.App)
	if err != nil {
		return fmt.Errorf("initialize migration logger: %w", err)
	}
	defer logger.Close()
	pool, err := database.NewMigration(context.Background(), cfg.MigrationPostgres)
	if err != nil {
		return fmt.Errorf("initialize migration postgres pool: %w", err)
	}
	defer pool.Close()
	items, err := migration.Load(migrations.Files)
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}
	result, err := migration.Run(context.Background(), pool, items)
	if err != nil {
		logger.Error("migration.failed", zap.Error(err))
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "failed"})
		return fmt.Errorf("execute migrations: %w", err)
	}
	logger.Info("migration.completed", zap.Int("applied", len(result.Applied)), zap.Int64("schema_version", result.SchemaVersion))
	return json.NewEncoder(os.Stdout).Encode(result)
}
