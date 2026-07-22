package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const rollbackTimeout = 5 * time.Second

var ErrDependencyMissing = errors.New("required postgres repository dependency is missing")

// DBTX is the PostgreSQL execution surface used by read-only diagnostics that
// may run against either a pool or a transaction. Mandatory writes must accept
// TransactionExecutor instead.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TransactionExecutor is implemented only by the platform wrapper around a
// PostgreSQL transaction. The private marker deliberately prevents Pool from
// satisfying mandatory-write repository constructors.
type TransactionExecutor interface {
	DBTX
	transactionScoped()
}

type transaction interface {
	DBTX
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type scopedTransactionExecutor struct{ transaction }

func (scopedTransactionExecutor) transactionScoped() {}

// WrapPGXTransaction adapts an explicitly opened pgx transaction for narrow
// infrastructure repositories. It never accepts pgxpool.Pool.
func WrapPGXTransaction(tx pgx.Tx) TransactionExecutor {
	if tx == nil {
		return nil
	}
	return scopedTransactionExecutor{transaction: tx}
}

type beginTransaction func(context.Context, pgx.TxOptions) (transaction, error)

// UnitOfWorkFactory creates one use-case-specific set of transaction-scoped
// adapters. The returned value must never be retained after the callback.
type UnitOfWorkFactory[T any] func(TransactionExecutor) (T, error)

// RollbackErrorObserver records a secondary rollback failure without replacing
// the callback error or recovered panic that caused the rollback.
type RollbackErrorObserver func(context.Context, error)

// TransactionRunner is the low-level PostgreSQL transaction owner. Concrete
// module/orchestration adapters wrap it behind narrow application contracts.
type TransactionRunner[T any] struct {
	begin             beginTransaction
	newUnitOfWork     UnitOfWorkFactory[T]
	onRollbackFailure RollbackErrorObserver
	options           pgx.TxOptions
}

func NewTransactionRunner[T any](pool *Pool, factory UnitOfWorkFactory[T], observer RollbackErrorObserver) (*TransactionRunner[T], error) {
	if pool == nil || pool.pool == nil || factory == nil {
		return nil, ErrDependencyMissing
	}
	return &TransactionRunner[T]{
		begin: func(ctx context.Context, options pgx.TxOptions) (transaction, error) {
			return pool.pool.BeginTx(ctx, options)
		},
		newUnitOfWork:     factory,
		onRollbackFailure: observer,
	}, nil
}

// WithinTransaction commits only after a successful callback. Callback errors,
// cancellation, and panics roll back. A panic is rethrown after rollback.
func (r *TransactionRunner[T]) WithinTransaction(ctx context.Context, fn func(context.Context, T) error) (err error) {
	if ctx == nil {
		return fmt.Errorf("transaction context is nil")
	}
	if fn == nil {
		return fmt.Errorf("transaction callback is nil")
	}
	if r.newUnitOfWork == nil {
		return fmt.Errorf("unit of work factory is nil")
	}

	options := r.options
	if options.IsoLevel == "" {
		options.IsoLevel = pgx.ReadCommitted
	}
	tx, err := r.begin(ctx, options)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	rollback := func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil && r.onRollbackFailure != nil {
			r.onRollbackFailure(rollbackCtx, rollbackErr)
		}
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			rollback()
			panic(recovered)
		}
	}()

	executor := scopedTransactionExecutor{transaction: tx}
	unitOfWork, err := r.newUnitOfWork(executor)
	if err != nil {
		rollback()
		return err
	}
	if err := fn(ctx, unitOfWork); err != nil {
		rollback()
		return err
	}
	if err := ctx.Err(); err != nil {
		rollback()
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		// pgx guarantees that Commit closes the transaction even when it
		// returns an error, so a second rollback is neither needed nor safe.
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
