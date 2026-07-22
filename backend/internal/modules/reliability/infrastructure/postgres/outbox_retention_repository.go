package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
)

type TransactionalOutboxRetentionRepository struct{ executor database.TransactionExecutor }

func NewTransactionalOutboxRetentionRepository(executor database.TransactionExecutor) (*TransactionalOutboxRetentionRepository, error) {
	if executor == nil {
		return nil, database.ErrDependencyMissing
	}
	return &TransactionalOutboxRetentionRepository{executor: executor}, nil
}

func (r *TransactionalOutboxRetentionRepository) DeleteProcessedBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	return r.deleteBefore(ctx, "delete_processed_outbox_events_before", before, limit)
}

func (r *TransactionalOutboxRetentionRepository) DeleteDeadLettersBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	return r.deleteBefore(ctx, "delete_dead_letter_outbox_events_before", before, limit)
}

func (r *TransactionalOutboxRetentionRepository) deleteBefore(ctx context.Context, function string, before time.Time, limit int) (int64, error) {
	if r == nil || r.executor == nil || before.IsZero() || limit < 1 || limit > outbox.MaxRetentionBatchSize {
		return 0, database.ErrDependencyMissing
	}
	var deleted int64
	query := "SELECT public." + function + "($1,$2)" // function is an internal constant.
	if err := r.executor.QueryRow(ctx, query, before, limit).Scan(&deleted); err != nil {
		return 0, fmt.Errorf("execute outbox retention batch: %w", err)
	}
	return deleted, nil
}

type OutboxRetentionTransactor struct {
	runner *database.TransactionRunner[outbox.RetentionRepository]
}

func NewOutboxRetentionTransactor(pool *database.Pool, observer database.RollbackErrorObserver) (*OutboxRetentionTransactor, error) {
	runner, err := database.NewTransactionRunner(
		pool,
		func(executor database.TransactionExecutor) (outbox.RetentionRepository, error) {
			return NewTransactionalOutboxRetentionRepository(executor)
		},
		observer,
	)
	if err != nil {
		return nil, err
	}
	return &OutboxRetentionTransactor{runner: runner}, nil
}

func (t *OutboxRetentionTransactor) WithinTransaction(ctx context.Context, fn func(context.Context, outbox.RetentionRepository) error) error {
	if t == nil || t.runner == nil {
		return database.ErrDependencyMissing
	}
	return t.runner.WithinTransaction(ctx, fn)
}

var _ outbox.RetentionRepository = (*TransactionalOutboxRetentionRepository)(nil)
var _ outbox.RetentionTransactor = (*OutboxRetentionTransactor)(nil)
