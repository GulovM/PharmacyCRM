package postgres

import (
	"context"
	"errors"
	"time"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	auditpostgres "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/infrastructure/postgres"
	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	outboxpostgres "github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/infrastructure/postgres"
	"github.com/GulovM/PharmacyCRM/backend/internal/orchestration/outboxreplay"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type unitOfWork struct {
	executor    database.TransactionExecutor
	idempotency *idempotency.Service
	outbox      *outboxpostgres.TransactionalOutboxRepository
	audit       *audit.Writer
}

func (u *unitOfWork) ClaimIdempotency(ctx context.Context, claim idempotency.Claim) (idempotency.ClaimResult, error) {
	if u == nil || u.idempotency == nil {
		return idempotency.ClaimResult{}, database.ErrDependencyMissing
	}
	return u.idempotency.Claim(ctx, claim)
}

func (u *unitOfWork) CompleteIdempotency(ctx context.Context, completion idempotency.Completion) error {
	if u == nil || u.idempotency == nil {
		return database.ErrDependencyMissing
	}
	return u.idempotency.Complete(ctx, completion)
}

func (u *unitOfWork) RevalidateAdmin(ctx context.Context, actor outboxreplay.Actor) error {
	if u == nil || u.executor == nil {
		return database.ErrDependencyMissing
	}
	var allowed bool
	err := u.executor.QueryRow(ctx, `
		SELECT true
		FROM users AS actor
		JOIN user_sessions AS session
		  ON session.id = $2 AND session.user_id = actor.id
		JOIN user_roles AS assignment
		  ON assignment.user_id = actor.id AND assignment.revoked_at IS NULL
		JOIN roles AS role ON role.id = assignment.role_id
		WHERE actor.id = $1
		  AND actor.status = 'ACTIVE'
		  AND session.revoked_at IS NULL
		  AND session.expires_at > statement_timestamp()
		  AND session.idle_expires_at > statement_timestamp()
		  AND session.absolute_expires_at > statement_timestamp()
		  AND session.mfa_level IN ('TOTP', 'WEBAUTHN', 'RECOVERY')
		  AND role.code = 'ADMIN'
		FOR UPDATE OF actor, session, assignment`, actor.UserID, actor.SessionID).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return &apperror.Typed{Category: apperror.ErrForbidden}
	}
	if err != nil {
		return err
	}
	if !allowed {
		return &apperror.Typed{Category: apperror.ErrForbidden}
	}
	return nil
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
	runner transactionExecutor
}

type transactionExecutor interface {
	WithinTransaction(context.Context, func(context.Context, outboxreplay.UnitOfWork) error) error
}

func NewTransactor(pool *database.Pool, observer database.RollbackErrorObserver) (*Transactor, error) {
	runner, err := database.NewTransactionRunner(
		pool,
		func(executor database.TransactionExecutor) (outboxreplay.UnitOfWork, error) {
			idempotencyRepository, err := outboxpostgres.NewTransactionalIdempotencyRepository(executor)
			if err != nil {
				return nil, err
			}
			idempotencyService, err := idempotency.NewService(idempotencyRepository)
			if err != nil {
				return nil, err
			}
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
			return &unitOfWork{executor: executor, idempotency: idempotencyService, outbox: outboxRepository, audit: writer}, nil
		},
		observer,
	)
	if err != nil {
		return nil, err
	}
	return &Transactor{runner: database.NewRetryingTransactionRunner[outboxreplay.UnitOfWork](runner, nil)}, nil
}

func (t *Transactor) WithinTransaction(ctx context.Context, fn func(context.Context, outboxreplay.UnitOfWork) error) error {
	if t == nil || t.runner == nil {
		return database.ErrDependencyMissing
	}
	return t.runner.WithinTransaction(ctx, fn)
}

var _ outboxreplay.Transactor = (*Transactor)(nil)
