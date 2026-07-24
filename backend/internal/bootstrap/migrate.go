package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/migration"
	embeddedmigrations "github.com/GulovM/PharmacyCRM/backend/migrations"
	"go.uber.org/zap"
)

type migrationProcessLogger interface {
	Info(string, ...zap.Field)
	Error(string, ...zap.Field)
	Close() error
}

type migrationProcessPool interface{ Close() }

type migrationDependencies struct {
	loadConfig     func() (config.MigrationConfig, error)
	newLogger      func(config.LoggingConfig, config.AppConfig) (migrationProcessLogger, error)
	newPool        func(context.Context, config.MigrationPostgresConfig) (migrationProcessPool, error)
	loadMigrations func() ([]migration.Migration, error)
	runMigrations  func(context.Context, migrationProcessPool, []migration.Migration) (migration.Result, error)
	encode         func(any) error
}

func defaultMigrationDependencies() migrationDependencies {
	return migrationDependencies{
		loadConfig: config.LoadMigration,
		newLogger: func(loggingConfig config.LoggingConfig, appConfig config.AppConfig) (migrationProcessLogger, error) {
			return logging.New(loggingConfig, appConfig)
		},
		newPool: func(ctx context.Context, postgresConfig config.MigrationPostgresConfig) (migrationProcessPool, error) {
			return database.NewMigration(ctx, postgresConfig)
		},
		loadMigrations: func() ([]migration.Migration, error) {
			return migration.Load(embeddedmigrations.Files)
		},
		runMigrations: func(ctx context.Context, pool migrationProcessPool, items []migration.Migration) (migration.Result, error) {
			databasePool, ok := pool.(*database.Pool)
			if !ok {
				return migration.Result{}, errors.New("migration pool has incompatible implementation")
			}
			return migration.Run(ctx, databasePool, items)
		},
		encode: func(value any) error { return json.NewEncoder(os.Stdout).Encode(value) },
	}
}

func RunMigration() error {
	return runMigration(defaultMigrationDependencies())
}

func runMigration(dependencies migrationDependencies) (resultErr error) {
	cfg, err := dependencies.loadConfig()
	if err != nil {
		return err
	}
	logger, err := dependencies.newLogger(cfg.Logging, cfg.App)
	if err != nil {
		return fmt.Errorf("initialize migration logger: %w", err)
	}
	defer func() {
		if closeErr := logger.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close migration logger: %w", closeErr))
		}
	}()

	pool, err := dependencies.newPool(context.Background(), cfg.MigrationPostgres)
	if err != nil {
		return fmt.Errorf("initialize migration postgres pool: %w", err)
	}
	defer pool.Close()
	items, err := dependencies.loadMigrations()
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}
	result, err := dependencies.runMigrations(context.Background(), pool, items)
	if err != nil {
		logger.Error("migration.failed", zap.Error(err))
		executionErr := fmt.Errorf("execute migrations: %w", err)
		if encodeErr := dependencies.encode(map[string]string{"status": "failed"}); encodeErr != nil {
			return errors.Join(executionErr, fmt.Errorf("encode migration failure: %w", encodeErr))
		}
		return executionErr
	}
	logger.Info("migration.completed", zap.Int("applied", len(result.Applied)), zap.Int64("schema_version", result.SchemaVersion))
	if err := dependencies.encode(result); err != nil {
		return fmt.Errorf("encode migration result: %w", err)
	}
	return nil
}
