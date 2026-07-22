package postgres

import (
	"context"
	"errors"
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
	return u.outbox.ReplayDeadLetter(ctx, id, at)
}

func (u *unitOfWork) AppendAudit(ctx context.Context, event audit.Event) error {
	return u.audit.Append(ctx, event)
}

type Transactor struct {
	runner *database.TransactionRunner[outboxreplay.UnitOfWork]
}

func NewTransactor(pool *database.Pool, observer database.RollbackErrorObserver) *Transactor {
	return &Transactor{runner: database.NewTransactionRunner(
		pool,
		func(executor database.TransactionExecutor) outboxreplay.UnitOfWork {
			return &unitOfWork{
				outbox: outboxpostgres.NewTransactionalOutboxRepository(executor),
				audit:  audit.NewWriter(auditpostgres.NewTransactionalAuditRepository(executor), outboxreplay.AuditMetadataPolicy()),
			}
		},
		observer,
	)}
}

func (t *Transactor) WithinTransaction(ctx context.Context, fn func(context.Context, outboxreplay.UnitOfWork) error) error {
	if t == nil || t.runner == nil {
		return errors.New("outbox replay transactor is not configured")
	}
	return t.runner.WithinTransaction(ctx, fn)
}

var _ outboxreplay.Transactor = (*Transactor)(nil)
