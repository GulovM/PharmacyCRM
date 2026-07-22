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

var (
	ErrWorkerSchemaIncompatible   = errors.New("worker schema is incompatible")
	ErrWorkerProtocolIncompatible = errors.New("worker protocol is incompatible")
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
	newPool    func(context.Context, config.WorkerPostgresConfig) (workerProcessPool, error)
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
		newPool: func(ctx context.Context, postgresConfig config.WorkerPostgresConfig) (workerProcessPool, error) {
			return database.NewWorker(ctx, postgresConfig)
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
		return fmt.Errorf("initialize worker logger: %w", err)
	}
	defer func() {
		if closeErr := logger.Close(); closeErr != nil {
			result = errors.Join(result, fmt.Errorf("close logger: %w", closeErr))
		}
	}()

	ctx, stop := dependencies.newContext()
	defer stop()
	pool, err := dependencies.newPool(ctx, cfg.WorkerPostgres)
	if err != nil {
		return fmt.Errorf("initialize worker postgres pool: %w", err)
	}
	defer pool.Close()

	version, err := pool.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("read worker schema version: %w", err)
	}
	if version < int64(cfg.App.MinSchemaVersion) || version > int64(cfg.App.MaxSchemaVersion) {
		return fmt.Errorf("%w: database=%d supported=%d..%d", ErrWorkerSchemaIncompatible, version, cfg.App.MinSchemaVersion, cfg.App.MaxSchemaVersion)
	}
	if cfg.Worker.ProtocolVersion != config.SupportedWorkerProtocol || cfg.Worker.ProtocolVersion != cfg.App.WorkerProtocol {
		return fmt.Errorf("%w: worker=%d application=%d supported=%d", ErrWorkerProtocolIncompatible, cfg.Worker.ProtocolVersion, cfg.App.WorkerProtocol, config.SupportedWorkerProtocol)
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
	if err != nil {
		return nil, nil, err
	}
	retentionObserver := &outboxRetentionObserver{logger: logger}
	retentionTransactor := reliabilitypostgres.NewOutboxRetentionTransactor(databasePool, func(_ context.Context, err error) {
		logger.Error("outbox.retention.transaction.rollback_failed", zap.Error(err))
	})
	retention, err := outbox.NewRetentionService(retentionTransactor, outbox.RetentionConfig{
		ProcessedFor: cfg.Worker.ProcessedRetention, DeadLettersFor: cfg.Worker.DeadLetterRetention,
		Interval: cfg.Worker.RetentionInterval, BatchSize: cfg.Worker.RetentionBatchSize,
		MaxBatchesPerCycle: cfg.Worker.RetentionMaxBatches, MaxCycleDuration: cfg.Worker.RetentionMaxDuration,
	}, retentionObserver)
	if err != nil {
		return nil, nil, err
	}
	return &outboxWorkerProcess{delivery: worker, retention: retention}, nil, nil
}

type outboxWorkerProcess struct {
	delivery  *outbox.Worker
	retention *outbox.RetentionService
}

func (p *outboxWorkerProcess) ValidateProtocols(protocols []outbox.EventKey) error {
	return p.delivery.ValidateProtocols(protocols)
}

func (p *outboxWorkerProcess) Run(ctx context.Context) error {
	processContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- p.delivery.Run(processContext) }()
	go func() { results <- p.retention.Run(processContext) }()
	first := <-results
	cancel()
	second := <-results
	return errors.Join(first, second)
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

type outboxRetentionObserver struct {
	logger           workerProcessLogger
	processedDeleted atomic.Uint64
	deadDeleted      atomic.Uint64
	cycleFailures    atomic.Uint64
}

func (o *outboxRetentionObserver) BatchDeleted(_ context.Context, status string, deleted int64) {
	if status == "PROCESSED" {
		o.processedDeleted.Add(uint64(deleted))
	} else {
		o.deadDeleted.Add(uint64(deleted))
	}
	o.logger.Info("outbox.retention.batch.completed", zap.String("status", status), zap.Int64("deleted", deleted))
}

func (o *outboxRetentionObserver) CycleFailed(_ context.Context, err error) {
	o.cycleFailures.Add(1)
	o.logger.Error("outbox.retention.cycle.failed", zap.Error(err))
}

func (o *outboxRetentionObserver) CycleCompleted(_ context.Context, stats outbox.RetentionCycleStats) {
	o.logger.Info("outbox.retention.cycle.completed", zap.Int64("processed_deleted", stats.ProcessedDeleted), zap.Int64("dead_letter_deleted", stats.DeadLetterDeleted), zap.Int("batches", stats.Batches), zap.Bool("limited", stats.Limited), zap.Duration("duration", stats.Duration))
}
