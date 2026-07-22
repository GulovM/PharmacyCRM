package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
	reliabilitypostgres "github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/infrastructure/postgres"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"go.uber.org/zap"
)

type workerProcessLogger interface {
	Info(string, ...zap.Field)
	Warn(string, ...zap.Field)
	Error(string, ...zap.Field)
	Close() error
}

type workerProcessPool interface {
	SchemaVersion(context.Context) (int64, error)
	Close()
}

type workerProcess interface {
	ValidateProtocols([]outbox.EventKey) error
	Run(context.Context) error
}

type workerDependencies struct {
	loadConfig func() (config.WorkerProcessConfig, error)
	newLogger  func(config.LoggingConfig, config.AppConfig) (workerProcessLogger, error)
	newContext func() (context.Context, context.CancelFunc)
	newPool    func(context.Context, config.RuntimePostgresConfig) (workerProcessPool, error)
	newWorker  func(workerProcessPool, config.WorkerProcessConfig, workerProcessLogger) (workerProcess, []outbox.EventKey, error)
}

func defaultWorkerDependencies() workerDependencies {
	return workerDependencies{
		loadConfig: config.LoadWorker,
		newLogger: func(loggingConfig config.LoggingConfig, appConfig config.AppConfig) (workerProcessLogger, error) {
			return logging.New(loggingConfig, appConfig)
		},
		newContext: func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		},
		newPool: func(ctx context.Context, postgresConfig config.RuntimePostgresConfig) (workerProcessPool, error) {
			return database.NewRuntime(ctx, postgresConfig)
		},
		newWorker: buildOutboxWorker,
	}
}

// RunWorker owns the complete worker process lifecycle and remains blocked
// until a signal, a fatal polling error, or a bounded shutdown failure.
func RunWorker() error {
	return runWorker(defaultWorkerDependencies())
}

func runWorker(dependencies workerDependencies) (result error) {
	cfg, err := dependencies.loadConfig()
	if err != nil {
		return err
	}
	logger, err := dependencies.newLogger(cfg.Logging, cfg.App)
	if err != nil {
		return fmt.Errorf("initialize logger")
	}
	defer func() {
		if closeErr := logger.Close(); closeErr != nil {
			result = errors.Join(result, fmt.Errorf("close logger: %w", closeErr))
		}
	}()

	ctx, stop := dependencies.newContext()
	defer stop()
	pool, err := dependencies.newPool(ctx, cfg.RuntimePostgres)
	if err != nil {
		return fmt.Errorf("initialize postgres pool")
	}
	defer pool.Close()

	version, err := pool.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("verify worker schema compatibility")
	}
	if version < int64(cfg.App.MinSchemaVersion) || version > int64(cfg.App.MaxSchemaVersion) {
		return fmt.Errorf("worker schema is incompatible")
	}
	if cfg.Worker.ProtocolVersion != config.SupportedWorkerProtocol || cfg.Worker.ProtocolVersion != cfg.App.WorkerProtocol {
		return fmt.Errorf("worker protocol is incompatible")
	}

	worker, requiredProtocols, err := dependencies.newWorker(pool, cfg, logger)
	if err != nil {
		return fmt.Errorf("initialize outbox worker: %w", err)
	}
	if err := worker.ValidateProtocols(requiredProtocols); err != nil {
		return fmt.Errorf("validate outbox protocols: %w", err)
	}
	if len(requiredProtocols) == 0 {
		logger.Warn("outbox.protocol_registry.empty", zap.String("readiness", "production_protocols_absent"))
	}
	logger.Info("worker.started", zap.String("owner", cfg.Worker.Owner), zap.Int64("schema_version", version))
	if err := worker.Run(ctx); err != nil {
		logger.Error("worker.failed", zap.Error(err))
		return fmt.Errorf("run outbox worker: %w", err)
	}
	logger.Info("worker.stopped")
	return nil
}

func buildOutboxWorker(pool workerProcessPool, cfg config.WorkerProcessConfig, logger workerProcessLogger) (workerProcess, []outbox.EventKey, error) {
	databasePool, ok := pool.(*database.Pool)
	if !ok {
		return nil, nil, errors.New("postgres pool has incompatible implementation")
	}
	observer := &outboxProcessObserver{logger: logger}
	transactor := reliabilitypostgres.NewOutboxTransactor(databasePool, func(_ context.Context, err error) {
		logger.Error("outbox.transaction.rollback_failed", zap.Error(err))
	})
	// E2 intentionally has no domain consumers. An empty registry is explicit:
	// ClaimBatch filters by registered protocols and therefore never acknowledges
	// an unknown business event.
	handlers := map[outbox.EventKey]outbox.Handler{}
	worker, err := outbox.NewWorker(transactor, handlers, outbox.WorkerConfig{
		Owner: cfg.Worker.Owner, Concurrency: cfg.Worker.Concurrency, MaxClaim: cfg.Worker.MaxClaim,
		PollInterval: cfg.Worker.PollInterval, LeaseDuration: cfg.Worker.LeaseDuration,
		DrainTimeout: cfg.Worker.DrainTimeout,
	}, observer, reliabilitypostgres.OutboxClaimErrorClassifier{})
	return worker, nil, err
}

type outboxProcessObserver struct {
	logger               workerProcessLogger
	claimFailures        atomic.Uint64
	finalizationFailures atomic.Uint64
	staleLeases          atomic.Uint64
	deadLetters          atomic.Uint64
}

func (o *outboxProcessObserver) ClaimFailed(_ context.Context, err error, transient bool, retryAfter time.Duration) {
	o.claimFailures.Add(1)
	o.logger.Warn("outbox.claim.failed", zap.Bool("transient", transient), zap.Duration("retry_after", retryAfter), zap.Error(err))
}
func (o *outboxProcessObserver) FinalizationFailed(_ context.Context, lease outbox.Lease, err error) {
	o.finalizationFailures.Add(1)
	o.logger.Error("outbox.finalization.failed", zap.String("event_id", lease.ID.String()), zap.Error(err))
}
func (o *outboxProcessObserver) StaleLease(_ context.Context, lease outbox.Lease) {
	o.staleLeases.Add(1)
	o.logger.Warn("outbox.lease.stale", zap.String("event_id", lease.ID.String()))
}
func (o *outboxProcessObserver) DeadLettered(_ context.Context, lease outbox.Lease, code string) {
	o.deadLetters.Add(1)
	o.logger.Error("outbox.event.dead_lettered", zap.String("event_id", lease.ID.String()), zap.String("error_code", code))
}
