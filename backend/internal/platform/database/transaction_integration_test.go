package database

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTransactionRunnerIntegration(t *testing.T) {
	dsn := postgrestest.DSN(t)

	ctx := context.Background()
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE uow_probe (value integer NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	runner := &TransactionRunner[TransactionExecutor]{
		begin: func(ctx context.Context, options pgx.TxOptions) (transaction, error) {
			return pool.BeginTx(ctx, options)
		},
		newUnitOfWork: func(executor TransactionExecutor) TransactionExecutor { return executor },
	}

	if err := runner.WithinTransaction(ctx, func(ctx context.Context, executor TransactionExecutor) error {
		_, err := executor.Exec(ctx, "INSERT INTO uow_probe (value) VALUES (1)")
		return err
	}); err != nil {
		t.Fatal(err)
	}

	callbackErr := errors.New("force rollback")
	if err := runner.WithinTransaction(ctx, func(ctx context.Context, executor TransactionExecutor) error {
		if _, err := executor.Exec(ctx, "INSERT INTO uow_probe (value) VALUES (2)"); err != nil {
			return err
		}
		return callbackErr
	}); !errors.Is(err, callbackErr) {
		t.Fatalf("expected callback error, got %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	if err := runner.WithinTransaction(cancelCtx, func(ctx context.Context, executor TransactionExecutor) error {
		if _, err := executor.Exec(ctx, "INSERT INTO uow_probe (value) VALUES (3)"); err != nil {
			return err
		}
		cancel()
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	var values []int32
	rows, err := pool.Query(ctx, "SELECT value FROM uow_probe ORDER BY value")
	if err != nil {
		t.Fatal(err)
	}
	values, err = pgx.CollectRows(rows, pgx.RowTo[int32])
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0] != 1 {
		t.Fatalf("unexpected committed values: %v", values)
	}
	if acquired := pool.Stat().AcquiredConns(); acquired != 0 {
		t.Fatalf("transaction leaked a connection: %d acquired", acquired)
	}
}

func TestRetryingTransactionRunnerIntegration(t *testing.T) {
	dsn := postgrestest.DSN(t)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS e2_retry_probe; CREATE TABLE e2_retry_probe (id integer PRIMARY KEY, value integer NOT NULL); INSERT INTO e2_retry_probe VALUES (1, 0)"); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DROP TABLE IF EXISTS e2_retry_probe")

	readDone, externalCommitted := make(chan struct{}), make(chan error, 1)
	go func() {
		<-readDone
		_, err := pool.Exec(ctx, "UPDATE e2_retry_probe SET value = value + 1 WHERE id = 1")
		externalCommitted <- err
	}()

	inner := &TransactionRunner[TransactionExecutor]{
		begin: func(ctx context.Context, options pgx.TxOptions) (transaction, error) {
			return pool.BeginTx(ctx, options)
		},
		newUnitOfWork: func(executor TransactionExecutor) TransactionExecutor { return executor },
		options:       pgx.TxOptions{IsoLevel: pgx.Serializable},
	}
	var attempt atomic.Int32
	runner := NewRetryingTransactionRunner[TransactionExecutor](inner, func(_ context.Context, current int) { attempt.Store(int32(current)) })
	runner.delay = func(int) time.Duration { return 0 }

	if err := runner.WithinTransaction(ctx, func(ctx context.Context, executor TransactionExecutor) error {
		var value int
		if err := executor.QueryRow(ctx, "SELECT value FROM e2_retry_probe WHERE id = 1").Scan(&value); err != nil {
			return err
		}
		if attempt.Load() == 1 {
			close(readDone)
			if err := <-externalCommitted; err != nil {
				return err
			}
		}
		_, err := executor.Exec(ctx, "UPDATE e2_retry_probe SET value = value + 10 WHERE id = 1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if attempt.Load() != 2 {
		t.Fatalf("expected two attempts, got %d", attempt.Load())
	}
	var value int
	if err := pool.QueryRow(ctx, "SELECT value FROM e2_retry_probe WHERE id = 1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 11 {
		t.Fatalf("whole-transaction retry produced value %d", value)
	}
}
