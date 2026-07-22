package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"go.uber.org/zap"
)

type fakeAPILogger struct {
	closeErr   error
	closeCalls int
}

func (*fakeAPILogger) Error(string, ...zap.Field) {}
func (l *fakeAPILogger) Close() error {
	l.closeCalls++
	return l.closeErr
}

type fakeAPIPool struct{ closeCalls int }

func (*fakeAPIPool) Ping(context.Context) error                 { return nil }
func (*fakeAPIPool) SchemaVersion(context.Context) (int64, error) { return 23, nil }
func (p *fakeAPIPool) Close()                                  { p.closeCalls++ }

type fakeAPIServer struct {
	serveErr, shutdownErr error
	block                  bool
}

func (s *fakeAPIServer) ListenAndServe() error {
	if s.block {
		select {}
	}
	return s.serveErr
}
func (s *fakeAPIServer) Shutdown(context.Context) error { return s.shutdownErr }

func baseAPIDependencies(logger apiProcessLogger, pool apiProcessPool, server apiProcessServer) apiDependencies {
	return apiDependencies{
		loadConfig: func() (config.APIConfig, error) { return config.APIConfig{}, nil },
		newLogger:  func(config.LoggingConfig, config.AppConfig) (apiProcessLogger, error) { return logger, nil },
		newContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
		newPool:    func(context.Context, config.APIPostgresConfig) (apiProcessPool, error) { return pool, nil },
		newServer:  func(apiProcessPool, config.APIConfig, apiProcessLogger) (apiProcessServer, error) { return server, nil },
	}
}

func TestRunAPIPreservesInitializationErrors(t *testing.T) {
	loadErr := errors.New("load")
	dependencies := baseAPIDependencies(&fakeAPILogger{}, &fakeAPIPool{}, &fakeAPIServer{})
	dependencies.loadConfig = func() (config.APIConfig, error) { return config.APIConfig{}, loadErr }
	if err := runAPI(dependencies); !errors.Is(err, loadErr) {
		t.Fatalf("load error=%v", err)
	}

	loggerErr := errors.New("logger")
	dependencies = baseAPIDependencies(&fakeAPILogger{}, &fakeAPIPool{}, &fakeAPIServer{})
	dependencies.newLogger = func(config.LoggingConfig, config.AppConfig) (apiProcessLogger, error) { return nil, loggerErr }
	if err := runAPI(dependencies); !errors.Is(err, loggerErr) {
		t.Fatalf("logger error=%v", err)
	}

	poolErr := errors.New("pool")
	logger := &fakeAPILogger{}
	dependencies = baseAPIDependencies(logger, &fakeAPIPool{}, &fakeAPIServer{})
	dependencies.newPool = func(context.Context, config.APIPostgresConfig) (apiProcessPool, error) { return nil, poolErr }
	if err := runAPI(dependencies); !errors.Is(err, poolErr) || logger.closeCalls != 1 {
		t.Fatalf("pool error=%v logger closes=%d", err, logger.closeCalls)
	}

	serverErr := errors.New("server")
	logger, pool := &fakeAPILogger{}, &fakeAPIPool{}
	dependencies = baseAPIDependencies(logger, pool, &fakeAPIServer{})
	dependencies.newServer = func(apiProcessPool, config.APIConfig, apiProcessLogger) (apiProcessServer, error) { return nil, serverErr }
	if err := runAPI(dependencies); !errors.Is(err, serverErr) || logger.closeCalls != 1 || pool.closeCalls != 1 {
		t.Fatalf("server error=%v logger closes=%d pool closes=%d", err, logger.closeCalls, pool.closeCalls)
	}
}

func TestRunAPIPreservesServeAndCleanupErrors(t *testing.T) {
	serveErr, closeErr := errors.New("serve"), errors.New("close")
	logger, pool := &fakeAPILogger{closeErr: closeErr}, &fakeAPIPool{}
	dependencies := baseAPIDependencies(logger, pool, &fakeAPIServer{serveErr: serveErr})
	err := runAPI(dependencies)
	if !errors.Is(err, serveErr) || !errors.Is(err, closeErr) || logger.closeCalls != 1 || pool.closeCalls != 1 {
		t.Fatalf("error=%v logger closes=%d pool closes=%d", err, logger.closeCalls, pool.closeCalls)
	}
}

func TestRunAPIPreservesShutdownError(t *testing.T) {
	shutdownErr := errors.New("shutdown")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dependencies := baseAPIDependencies(&fakeAPILogger{}, &fakeAPIPool{}, &fakeAPIServer{block: true, shutdownErr: shutdownErr})
	dependencies.newContext = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
	if err := runAPI(dependencies); !errors.Is(err, shutdownErr) {
		t.Fatalf("shutdown error=%v", err)
	}
}
