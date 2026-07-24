package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/migration"
	"go.uber.org/zap"
)

type fakeMigrationLogger struct {
	closeErr   error
	closeCalls int
}

func (*fakeMigrationLogger) Info(string, ...zap.Field)  {}
func (*fakeMigrationLogger) Error(string, ...zap.Field) {}
func (l *fakeMigrationLogger) Close() error {
	l.closeCalls++
	return l.closeErr
}

type fakeMigrationPool struct{ closeCalls int }

func (p *fakeMigrationPool) Close() { p.closeCalls++ }

func baseMigrationDependencies(logger migrationProcessLogger, pool migrationProcessPool) migrationDependencies {
	return migrationDependencies{
		loadConfig: func() (config.MigrationConfig, error) { return config.MigrationConfig{}, nil },
		newLogger:  func(config.LoggingConfig, config.AppConfig) (migrationProcessLogger, error) { return logger, nil },
		newPool:    func(context.Context, config.MigrationPostgresConfig) (migrationProcessPool, error) { return pool, nil },
		loadMigrations: func() ([]migration.Migration, error) {
			return []migration.Migration{{Version: 1, Name: "test", VerificationSQL: "SELECT true;"}}, nil
		},
		runMigrations: func(context.Context, migrationProcessPool, []migration.Migration) (migration.Result, error) {
			return migration.Result{SchemaVersion: 1}, nil
		},
		encode: func(any) error { return nil },
	}
}

func TestRunMigrationPreservesInitializationErrors(t *testing.T) {
	loadErr := errors.New("load config")
	dependencies := baseMigrationDependencies(&fakeMigrationLogger{}, &fakeMigrationPool{})
	dependencies.loadConfig = func() (config.MigrationConfig, error) { return config.MigrationConfig{}, loadErr }
	if err := runMigration(dependencies); !errors.Is(err, loadErr) {
		t.Fatalf("load error=%v", err)
	}

	loggerErr := errors.New("logger")
	dependencies = baseMigrationDependencies(&fakeMigrationLogger{}, &fakeMigrationPool{})
	dependencies.newLogger = func(config.LoggingConfig, config.AppConfig) (migrationProcessLogger, error) { return nil, loggerErr }
	if err := runMigration(dependencies); !errors.Is(err, loggerErr) {
		t.Fatalf("logger error=%v", err)
	}

	poolErr := errors.New("pool")
	logger := &fakeMigrationLogger{}
	dependencies = baseMigrationDependencies(logger, &fakeMigrationPool{})
	dependencies.newPool = func(context.Context, config.MigrationPostgresConfig) (migrationProcessPool, error) {
		return nil, poolErr
	}
	if err := runMigration(dependencies); !errors.Is(err, poolErr) || logger.closeCalls != 1 {
		t.Fatalf("pool error=%v logger closes=%d", err, logger.closeCalls)
	}
}

func TestRunMigrationPreservesLoadRunAndEncodeErrors(t *testing.T) {
	migrationLoadErr := errors.New("load migrations")
	logger, pool := &fakeMigrationLogger{}, &fakeMigrationPool{}
	dependencies := baseMigrationDependencies(logger, pool)
	dependencies.loadMigrations = func() ([]migration.Migration, error) { return nil, migrationLoadErr }
	if err := runMigration(dependencies); !errors.Is(err, migrationLoadErr) || logger.closeCalls != 1 || pool.closeCalls != 1 {
		t.Fatalf("migration load error=%v logger closes=%d pool closes=%d", err, logger.closeCalls, pool.closeCalls)
	}

	runErr, encodeErr, closeErr := errors.New("run"), errors.New("encode"), errors.New("close")
	logger, pool = &fakeMigrationLogger{closeErr: closeErr}, &fakeMigrationPool{}
	dependencies = baseMigrationDependencies(logger, pool)
	dependencies.runMigrations = func(context.Context, migrationProcessPool, []migration.Migration) (migration.Result, error) {
		return migration.Result{}, runErr
	}
	dependencies.encode = func(any) error { return encodeErr }
	err := runMigration(dependencies)
	if !errors.Is(err, runErr) || !errors.Is(err, encodeErr) || !errors.Is(err, closeErr) || logger.closeCalls != 1 || pool.closeCalls != 1 {
		t.Fatalf("run error=%v logger closes=%d pool closes=%d", err, logger.closeCalls, pool.closeCalls)
	}

	resultEncodeErr := errors.New("result encode")
	dependencies = baseMigrationDependencies(&fakeMigrationLogger{}, &fakeMigrationPool{})
	dependencies.encode = func(any) error { return resultEncodeErr }
	if err := runMigration(dependencies); !errors.Is(err, resultEncodeErr) {
		t.Fatalf("result encode error=%v", err)
	}
}
