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
	server, err := httpserver.New(cfg.HTTP, cfg.ProxyCORS, logger, httpserver.NewReadiness(pool))
	if err != nil {
		fmt.Fprintln(os.Stderr, "initialize http server")
		os.Exit(1)
	}
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
