package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeTransaction struct {
	commitErr, rollbackErr     error
	commitCalls, rollbackCalls int
}

func (f *fakeTransaction) Commit(context.Context) error   { f.commitCalls++; return f.commitErr }
func (f *fakeTransaction) Rollback(context.Context) error { f.rollbackCalls++; return f.rollbackErr }
func (f *fakeTransaction) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f *fakeTransaction) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (f *fakeTransaction) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }

func testTransactionRunner(tx *fakeTransaction, observer RollbackErrorObserver) *TransactionRunner[DBTX] {
	return &TransactionRunner[DBTX]{
		begin:             func(context.Context, pgx.TxOptions) (transaction, error) { return tx, nil },
		newUnitOfWork:     func(executor DBTX) DBTX { return executor },
		onRollbackFailure: observer,
	}
}

func TestTransactionRunnerCommitsSuccessfulCallback(t *testing.T) {
	tx := &fakeTransaction{}
	err := testTransactionRunner(tx, nil).WithinTransaction(context.Background(), func(_ context.Context, executor DBTX) error {
		if executor != tx {
			t.Fatal("callback received a different transaction")
		}
		return nil
	})
	if err != nil || tx.commitCalls != 1 || tx.rollbackCalls != 0 {
		t.Fatalf("err=%v commit=%d rollback=%d", err, tx.commitCalls, tx.rollbackCalls)
	}
}

func TestTransactionRunnerRollsBackAndPreservesCallbackError(t *testing.T) {
	callbackErr, rollbackErr := errors.New("callback"), errors.New("rollback")
	tx := &fakeTransaction{rollbackErr: rollbackErr}
	var observed error
	err := testTransactionRunner(tx, func(_ context.Context, err error) { observed = err }).WithinTransaction(context.Background(), func(context.Context, DBTX) error { return callbackErr })
	if !errors.Is(err, callbackErr) || !errors.Is(observed, rollbackErr) || tx.rollbackCalls != 1 || tx.commitCalls != 0 {
		t.Fatalf("err=%v observed=%v commit=%d rollback=%d", err, observed, tx.commitCalls, tx.rollbackCalls)
	}
}

func TestTransactionRunnerRollsBackAndRethrowsPanic(t *testing.T) {
	tx := &fakeTransaction{}
	deferred := func() (recovered any) {
		defer func() { recovered = recover() }()
		_ = testTransactionRunner(tx, nil).WithinTransaction(context.Background(), func(context.Context, DBTX) error { panic("boom") })
		return nil
	}()
	if deferred != "boom" || tx.rollbackCalls != 1 || tx.commitCalls != 0 {
		t.Fatalf("panic=%v commit=%d rollback=%d", deferred, tx.commitCalls, tx.rollbackCalls)
	}
}

func TestTransactionRunnerReturnsCommitFailure(t *testing.T) {
	commitErr := errors.New("commit")
	tx := &fakeTransaction{commitErr: commitErr}
	err := testTransactionRunner(tx, nil).WithinTransaction(context.Background(), func(context.Context, DBTX) error { return nil })
	if !errors.Is(err, commitErr) || tx.commitCalls != 1 || tx.rollbackCalls != 0 {
		t.Fatalf("err=%v commit=%d rollback=%d", err, tx.commitCalls, tx.rollbackCalls)
	}
}

func TestTransactionRunnerCancellationRollsBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tx := &fakeTransaction{}
	err := testTransactionRunner(tx, nil).WithinTransaction(ctx, func(context.Context, DBTX) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) || tx.rollbackCalls != 1 || tx.commitCalls != 0 {
		t.Fatalf("err=%v commit=%d rollback=%d", err, tx.commitCalls, tx.rollbackCalls)
	}
}

func TestTransactionRunnerRejectsInvalidInvocationBeforeBegin(t *testing.T) {
	tx := &fakeTransaction{}
	runner := testTransactionRunner(tx, nil)
	if runner.WithinTransaction(nil, func(context.Context, DBTX) error { return nil }) == nil {
		t.Fatal("expected nil context error")
	}
	if runner.WithinTransaction(context.Background(), nil) == nil {
		t.Fatal("expected nil callback error")
	}
	runner.newUnitOfWork = nil
	if runner.WithinTransaction(context.Background(), func(context.Context, DBTX) error { return nil }) == nil {
		t.Fatal("expected nil factory error")
	}
	if tx.commitCalls != 0 || tx.rollbackCalls != 0 {
		t.Fatal("transaction unexpectedly used")
	}
}
