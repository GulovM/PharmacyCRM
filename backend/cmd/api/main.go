package main

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
	pool, err := database.NewRuntime(context.Background(), cfg.Postgres)
	if err != nil {
		fmt.Fprintln(os.Stderr, "initialize postgres pool")
		os.Exit(1)
	}
	defer pool.Close()
	readiness := httpserver.NewReadiness(
		pool,
		func(ctx context.Context) error {
			version, err := pool.SchemaVersion(ctx)
			if err != nil || (cfg.App.MinSchemaVersion != 0 && version < int64(cfg.App.MinSchemaVersion)) || (cfg.App.MaxSchemaVersion != 0 && version > int64(cfg.App.MaxSchemaVersion)) {
				return errors.New("schema is incompatible")
			}
			return nil
		},
		func(context.Context) error {
			if cfg.Worker.ProtocolVersion != cfg.App.WorkerProtocol {
				return errors.New("worker protocol is incompatible")
			}
			return nil
		},
		func(context.Context) error { return nil },
	)
	server, err := httpserver.New(cfg.HTTP, cfg.ProxyCORS, logger, readiness)
	if err != nil {
		fmt.Fprintln(os.Stderr, "initialize http server")
		os.Exit(1)
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
			os.Exit(1)
		}
	case <-ctx.Done():
		if err := server.Shutdown(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("http.server.shutdown_failed", zap.Error(err))
			os.Exit(1)
		}
	}
}
