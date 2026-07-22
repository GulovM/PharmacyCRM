// Package bootstrap is the sole composition root for backend executables.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/httpserver"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"go.uber.org/zap"
)

type apiProcessLogger interface {
	Error(string, ...zap.Field)
	Close() error
}

type apiProcessPool interface {
	Ping(context.Context) error
	SchemaVersion(context.Context) (int64, error)
	Close()
}

type apiProcessServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

type apiDependencies struct {
	loadConfig func() (config.APIConfig, error)
	newLogger  func(config.LoggingConfig, config.AppConfig) (apiProcessLogger, error)
	newContext func() (context.Context, context.CancelFunc)
	newPool    func(context.Context, config.APIPostgresConfig) (apiProcessPool, error)
	newServer  func(apiProcessPool, config.APIConfig, apiProcessLogger) (apiProcessServer, error)
}

func defaultAPIDependencies() apiDependencies {
	return apiDependencies{
		loadConfig: config.LoadAPI,
		newLogger: func(loggingConfig config.LoggingConfig, appConfig config.AppConfig) (apiProcessLogger, error) {
			return logging.New(loggingConfig, appConfig)
		},
		newContext: func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		},
		newPool: func(ctx context.Context, postgresConfig config.APIPostgresConfig) (apiProcessPool, error) {
			return database.NewAPI(ctx, postgresConfig)
		},
		newServer: buildAPIServer,
	}
}

func RunAPI() error {
	return runAPI(defaultAPIDependencies())
}

func runAPI(dependencies apiDependencies) (result error) {
	cfg, err := dependencies.loadConfig()
	if err != nil {
		return err
	}
	logger, err := dependencies.newLogger(cfg.Logging, cfg.App)
	if err != nil {
		return fmt.Errorf("initialize API logger: %w", err)
	}
	defer func() {
		if closeErr := logger.Close(); closeErr != nil {
			result = errors.Join(result, fmt.Errorf("close API logger: %w", closeErr))
		}
	}()

	ctx, stop := dependencies.newContext()
	defer stop()
	pool, err := dependencies.newPool(ctx, cfg.APIPostgres)
	if err != nil {
		return fmt.Errorf("initialize API postgres pool: %w", err)
	}
	defer pool.Close()

	server, err := dependencies.newServer(pool, cfg, logger)
	if err != nil {
		return fmt.Errorf("initialize HTTP server: %w", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("http.server.failed", zap.Error(err))
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		if err := server.Shutdown(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("http.server.shutdown_failed", zap.Error(err))
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		return nil
	}
}

func buildAPIServer(pool apiProcessPool, cfg config.APIConfig, logger apiProcessLogger) (apiProcessServer, error) {
	databasePool, ok := pool.(*database.Pool)
	if !ok {
		return nil, errors.New("postgres pool has incompatible implementation")
	}
	concreteLogger, ok := logger.(*logging.Logger)
	if !ok {
		return nil, errors.New("logger has incompatible implementation")
	}
	readiness := httpserver.NewReadiness(
		databasePool,
		func(ctx context.Context) error {
			version, err := databasePool.SchemaVersion(ctx)
			if err != nil || version < int64(cfg.App.MinSchemaVersion) || version > int64(cfg.App.MaxSchemaVersion) {
				return errors.New("schema is incompatible")
			}
			return nil
		},
		func(context.Context) error {
			if cfg.Worker.ProtocolVersion != config.SupportedWorkerProtocol {
				return errors.New("worker protocol is incompatible")
			}
			return nil
		},
		func(context.Context) error { return nil },
	)
	server, err := httpserver.New(cfg.HTTP, cfg.ProxyCORS, concreteLogger, readiness)
	if err != nil {
		return nil, err
	}
	readiness.MarkStartupComplete()
	return server, nil
}
