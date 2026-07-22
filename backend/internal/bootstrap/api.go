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

func RunAPI() error {
	cfg, err := config.LoadAPI()
	if err != nil {
		return err
	}
	logger, err := logging.New(cfg.Logging, cfg.App)
	if err != nil {
		return fmt.Errorf("initialize logger")
	}
	defer logger.Close()
	pool, err := database.NewAPI(context.Background(), cfg.APIPostgres)
	if err != nil {
		return fmt.Errorf("initialize postgres pool")
	}
	defer pool.Close()
	readiness := httpserver.NewReadiness(
		pool,
		func(ctx context.Context) error {
			version, err := pool.SchemaVersion(ctx)
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
	server, err := httpserver.New(cfg.HTTP, cfg.ProxyCORS, logger, readiness)
	if err != nil {
		return fmt.Errorf("initialize http server")
	}
	readiness.MarkStartupComplete()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("http.server.failed", zap.Error(err))
			return fmt.Errorf("http server failed")
		}
		return nil
	case <-ctx.Done():
		if err := server.Shutdown(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("http.server.shutdown_failed", zap.Error(err))
			return fmt.Errorf("http server shutdown failed")
		}
		return nil
	}
}
