package database

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type scriptedTransactionExecutor[T any] struct {
	errors []error
	calls  int
	uow    T
}

func (s *scriptedTransactionExecutor[T]) WithinTransaction(ctx context.Context, fn func(context.Context, T) error) error {
	s.calls++
	if err := fn(ctx, s.uow); err != nil {
		return err
	}
	if s.calls <= len(s.errors) {
		return s.errors[s.calls-1]
	}
	return nil
}

func postgresError(code string) error { return &pgconn.PgError{Code: code} }

func TestRetryClassifierAcceptsOnlyApprovedSQLStates(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		if !IsRetryableTransactionError(fmt.Errorf("wrapped: %w", postgresError(code))) {
			t.Fatalf("expected %s to be retryable", code)
		}
	}
	for _, err := range []error{errors.New("domain conflict"), postgresError("23505"), postgresError("57014")} {
		if IsRetryableTransactionError(err) {
			t.Fatalf("unexpected retryable error: %v", err)
		}
	}
}

func TestRetryRunnerRepeatsWholeCallbackAndReportsAttempts(t *testing.T) {
	inner := &scriptedTransactionExecutor[int]{errors: []error{postgresError("40001"), postgresError("40P01"), nil}, uow: 7}
	attempts, callbackCalls := []int{}, 0
	runner := NewRetryingTransactionRunner[int](inner, func(_ context.Context, attempt int) { attempts = append(attempts, attempt) })
	runner.delay = func(int) time.Duration { return 0 }
	err := runner.WithinTransaction(context.Background(), func(_ context.Context, value int) error {
		callbackCalls++
		if value != 7 {
			t.Fatal("unstable unit of work")
		}
		return nil
	})
	if err != nil || inner.calls != 3 || callbackCalls != 3 || fmt.Sprint(attempts) != "[1 2 3]" {
		t.Fatalf("err=%v transactions=%d callbacks=%d attempts=%v", err, inner.calls, callbackCalls, attempts)
	}
}

func TestRetryRunnerStopsAtBudget(t *testing.T) {
	inner := &scriptedTransactionExecutor[struct{}]{errors: []error{postgresError("40001"), postgresError("40001"), postgresError("40001")}}
	runner := NewRetryingTransactionRunner[struct{}](inner, nil)
	runner.delay = func(int) time.Duration { return 0 }
	if err := runner.WithinTransaction(context.Background(), func(context.Context, struct{}) error { return nil }); !IsRetryableTransactionError(err) || inner.calls != MaxTransactionAttempts {
		t.Fatalf("err=%v calls=%d", err, inner.calls)
	}
}

func TestRetryRunnerDoesNotRetryDomainError(t *testing.T) {
	domainErr := errors.New("insufficient stock")
	inner := &scriptedTransactionExecutor[struct{}]{errors: []error{domainErr}}
	runner := NewRetryingTransactionRunner[struct{}](inner, nil)
	if err := runner.WithinTransaction(context.Background(), func(context.Context, struct{}) error { return nil }); !errors.Is(err, domainErr) || inner.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, inner.calls)
	}
}

func TestRetryRunnerHonorsCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inner := &scriptedTransactionExecutor[struct{}]{errors: []error{postgresError("40001")}}
	runner := NewRetryingTransactionRunner[struct{}](inner, nil)
	runner.delay = func(int) time.Duration { cancel(); return time.Hour }
	if err := runner.WithinTransaction(ctx, func(context.Context, struct{}) error { return nil }); !errors.Is(err, context.Canceled) || inner.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, inner.calls)
	}
}

func TestFullJitterDelayStaysWithinPublishedBounds(t *testing.T) {
	for attempt, maximum := range map[int]time.Duration{1: 25 * time.Millisecond, 2: 50 * time.Millisecond, 3: 100 * time.Millisecond, 8: retryDelayCap} {
		for range 100 {
			if delay := fullJitterDelay(attempt); delay < 0 || delay > maximum {
				t.Fatalf("attempt=%d delay=%v maximum=%v", attempt, delay, maximum)
			}
		}
	}
}

func TestRetryRunnerRejectsInvalidInvocation(t *testing.T) {
	runner := NewRetryingTransactionRunner[struct{}](&scriptedTransactionExecutor[struct{}]{}, nil)
	if runner.WithinTransaction(nil, func(context.Context, struct{}) error { return nil }) == nil {
		t.Fatal("expected nil context error")
	}
	if runner.WithinTransaction(context.Background(), nil) == nil {
		t.Fatal("expected nil callback error")
	}
	var nilRunner *RetryingTransactionRunner[struct{}]
	if nilRunner.WithinTransaction(context.Background(), func(context.Context, struct{}) error { return nil }) == nil {
		t.Fatal("expected nil executor error")
	}
}
