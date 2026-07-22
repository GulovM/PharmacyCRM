package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
)

type TransactionalOutboxRetentionRepository struct{ executor database.TransactionExecutor }

func NewTransactionalOutboxRetentionRepository(executor database.TransactionExecutor) *TransactionalOutboxRetentionRepository {
	return &TransactionalOutboxRetentionRepository{executor: executor}
}

func (r *TransactionalOutboxRetentionRepository) DeleteProcessedBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	return r.deleteBefore(ctx, "delete_processed_outbox_events_before", before, limit)
}

func (r *TransactionalOutboxRetentionRepository) DeleteDeadLettersBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	return r.deleteBefore(ctx, "delete_dead_letter_outbox_events_before", before, limit)
}

func (r *TransactionalOutboxRetentionRepository) deleteBefore(ctx context.Context, function string, before time.Time, limit int) (int64, error) {
	if r == nil || r.executor == nil || before.IsZero() || limit < 1 || limit > outbox.MaxRetentionBatchSize {
		return 0, errors.New("invalid outbox retention request")
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

func NewOutboxRetentionTransactor(pool *database.Pool, observer database.RollbackErrorObserver) *OutboxRetentionTransactor {
	return &OutboxRetentionTransactor{runner: database.NewTransactionRunner(
		pool,
		func(executor database.TransactionExecutor) outbox.RetentionRepository {
			return NewTransactionalOutboxRetentionRepository(executor)
		},
		observer,
	)}
}

func (t *OutboxRetentionTransactor) WithinTransaction(ctx context.Context, fn func(context.Context, outbox.RetentionRepository) error) error {
	if t == nil || t.runner == nil {
		return errors.New("outbox retention transactor is not configured")
	}
	return t.runner.WithinTransaction(ctx, fn)
}

var _ outbox.RetentionRepository = (*TransactionalOutboxRetentionRepository)(nil)
var _ outbox.RetentionTransactor = (*OutboxRetentionTransactor)(nil)
