package postgres

import (
	"context"
	"time"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	auditpostgres "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/infrastructure/postgres"
	outboxpostgres "github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/infrastructure/postgres"
	"github.com/GulovM/PharmacyCRM/backend/internal/orchestration/outboxreplay"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/google/uuid"
)

type unitOfWork struct {
	outbox *outboxpostgres.TransactionalOutboxRepository
	audit  *audit.Writer
}

func (u *unitOfWork) ReplayDeadLetter(ctx context.Context, id uuid.UUID, at time.Time) error {
	if u == nil || u.outbox == nil {
		return database.ErrDependencyMissing
	}
	return u.outbox.ReplayDeadLetter(ctx, id, at)
}

func (u *unitOfWork) AppendAudit(ctx context.Context, event audit.Event) error {
	if u == nil || u.audit == nil {
		return database.ErrDependencyMissing
	}
	return u.audit.Append(ctx, event)
}

type Transactor struct {
	runner *database.TransactionRunner[outboxreplay.UnitOfWork]
}

func NewTransactor(pool *database.Pool, observer database.RollbackErrorObserver) (*Transactor, error) {
	runner, err := database.NewTransactionRunner(
		pool,
		func(executor database.TransactionExecutor) (outboxreplay.UnitOfWork, error) {
			outboxRepository, err := outboxpostgres.NewTransactionalOutboxRepository(executor)
			if err != nil {
				return nil, err
			}
			auditRepository, err := auditpostgres.NewTransactionalAuditRepository(executor)
			if err != nil {
				return nil, err
			}
			writer, err := audit.NewWriter(auditRepository, outboxreplay.AuditMetadataPolicy())
			if err != nil {
				return nil, err
			}
			return &unitOfWork{outbox: outboxRepository, audit: writer}, nil
		},
		observer,
	)
	if err != nil {
		return nil, err
	}
	return &Transactor{runner: runner}, nil
}

func (t *Transactor) WithinTransaction(ctx context.Context, fn func(context.Context, outboxreplay.UnitOfWork) error) error {
	if t == nil || t.runner == nil {
		return database.ErrDependencyMissing
	}
	return t.runner.WithinTransaction(ctx, fn)
}

var _ outboxreplay.Transactor = (*Transactor)(nil)
