package database

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTransactionRunnerIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}

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
	runner := &TransactionRunner[DBTX]{
		begin: func(ctx context.Context, options pgx.TxOptions) (transaction, error) {
			return pool.BeginTx(ctx, options)
		},
		newUnitOfWork: func(executor DBTX) DBTX { return executor },
	}

	if err := runner.WithinTransaction(ctx, func(ctx context.Context, executor DBTX) error {
		_, err := executor.Exec(ctx, "INSERT INTO uow_probe (value) VALUES (1)")
		return err
	}); err != nil {
		t.Fatal(err)
	}

	callbackErr := errors.New("force rollback")
	if err := runner.WithinTransaction(ctx, func(ctx context.Context, executor DBTX) error {
		if _, err := executor.Exec(ctx, "INSERT INTO uow_probe (value) VALUES (2)"); err != nil {
			return err
		}
		return callbackErr
	}); !errors.Is(err, callbackErr) {
		t.Fatalf("expected callback error, got %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	if err := runner.WithinTransaction(cancelCtx, func(ctx context.Context, executor DBTX) error {
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
