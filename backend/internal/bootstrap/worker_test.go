package bootstrap

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"go.uber.org/zap"
)

type fakeWorkerLogger struct{ closed atomic.Bool }

func (*fakeWorkerLogger) Info(string, ...zap.Field)  {}
func (*fakeWorkerLogger) Warn(string, ...zap.Field)  {}
func (*fakeWorkerLogger) Error(string, ...zap.Field) {}
func (l *fakeWorkerLogger) Close() error             { l.closed.Store(true); return nil }

type fakeWorkerPool struct {
	version int64
	err     error
	closed  atomic.Bool
}

func (p *fakeWorkerPool) SchemaVersion(context.Context) (int64, error) { return p.version, p.err }
func (p *fakeWorkerPool) Close()                                       { p.closed.Store(true) }

type fakeWorkerProcess struct {
	run      func(context.Context) error
	validate func([]outbox.EventKey) error
}

func (w *fakeWorkerProcess) Run(ctx context.Context) error { return w.run(ctx) }
func (w *fakeWorkerProcess) ValidateProtocols(protocols []outbox.EventKey) error {
	if w.validate != nil {
		return w.validate(protocols)
	}
	return nil
}

func validWorkerProcessConfig() config.WorkerProcessConfig {
	return config.WorkerProcessConfig{
		App:    config.AppConfig{MinSchemaVersion: 14, MaxSchemaVersion: 14, WorkerProtocol: 1},
		Worker: config.WorkerConfig{ProtocolVersion: 1, Owner: "worker-test-1", Concurrency: 1, MaxClaim: 1, PollInterval: time.Millisecond, LeaseDuration: time.Second, DrainTimeout: time.Second},
	}
}

func testWorkerDependencies(ctx context.Context, cancel context.CancelFunc, logger *fakeWorkerLogger, pool *fakeWorkerPool, worker workerProcess) workerDependencies {
	return workerDependencies{
		loadConfig: func() (config.WorkerProcessConfig, error) { return validWorkerProcessConfig(), nil },
		newLogger:  func(config.LoggingConfig, config.AppConfig) (workerProcessLogger, error) { return logger, nil },
		newContext: func() (context.Context, context.CancelFunc) { return ctx, cancel },
		newPool:    func(context.Context, config.RuntimePostgresConfig) (workerProcessPool, error) { return pool, nil },
		newWorker: func(workerProcessPool, config.WorkerProcessConfig, workerProcessLogger) (workerProcess, []outbox.EventKey, error) {
			return worker, nil, nil
		},
	}
}

func TestWorkerBootstrapStaysRunningUntilCancellationAndClosesResources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := &fakeWorkerLogger{}
	pool := &fakeWorkerPool{version: 14}
	started := make(chan struct{})
	worker := &fakeWorkerProcess{run: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- runWorker(testWorkerDependencies(ctx, cancel, logger, pool, worker)) }()
	<-started
	select {
	case err := <-done:
		t.Fatalf("worker bootstrap returned before cancellation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("graceful worker shutdown returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker bootstrap did not stop within bound")
	}
	if !pool.closed.Load() || !logger.closed.Load() {
		t.Fatalf("resources not closed: pool=%t logger=%t", pool.closed.Load(), logger.closed.Load())
	}
}

func TestWorkerBootstrapRejectsInvalidConfigBeforeAllocatingResources(t *testing.T) {
	configErr := errors.New("invalid worker configuration")
	loggerCalled := false
	dependencies := workerDependencies{
		loadConfig: func() (config.WorkerProcessConfig, error) { return config.WorkerProcessConfig{}, configErr },
		newLogger: func(config.LoggingConfig, config.AppConfig) (workerProcessLogger, error) {
			loggerCalled = true
			return nil, nil
		},
	}
	if err := runWorker(dependencies); !errors.Is(err, configErr) || loggerCalled {
		t.Fatalf("err=%v logger_called=%t", err, loggerCalled)
	}
}

func TestWorkerBootstrapRejectsUnsupportedRequiredProtocol(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	protocolErr := errors.New("unsupported protocol")
	logger := &fakeWorkerLogger{}
	pool := &fakeWorkerPool{version: 14}
	worker := &fakeWorkerProcess{
		run:      func(context.Context) error { t.Fatal("worker must not run"); return nil },
		validate: func([]outbox.EventKey) error { return protocolErr },
	}
	dependencies := testWorkerDependencies(ctx, cancel, logger, pool, worker)
	dependencies.newWorker = func(workerProcessPool, config.WorkerProcessConfig, workerProcessLogger) (workerProcess, []outbox.EventKey, error) {
		return worker, []outbox.EventKey{{Name: "inventory.changed", Version: 2}}, nil
	}
	if err := runWorker(dependencies); !errors.Is(err, protocolErr) {
		t.Fatalf("expected protocol error, got %v", err)
	}
	if !pool.closed.Load() || !logger.closed.Load() {
		t.Fatal("startup failure did not close resources")
	}
}

func TestWorkerBootstrapReportsUnavailablePostgresAndClosesLogger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := &fakeWorkerLogger{}
	poolErr := errors.New("database unavailable")
	dependencies := testWorkerDependencies(ctx, cancel, logger, nil, nil)
	dependencies.newPool = func(context.Context, config.RuntimePostgresConfig) (workerProcessPool, error) { return nil, poolErr }
	if err := runWorker(dependencies); err == nil {
		t.Fatal("expected controlled postgres startup error")
	}
	if !logger.closed.Load() {
		t.Fatal("logger was not closed after postgres startup failure")
	}
}
