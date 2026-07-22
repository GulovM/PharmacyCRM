package database

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	MaxTransactionAttempts = 3
	retryBaseDelay         = 25 * time.Millisecond
	retryDelayCap          = 250 * time.Millisecond
)

type transactionExecutor[T any] interface {
	WithinTransaction(context.Context, func(context.Context, T) error) error
}

// AttemptObserver receives a one-based attempt number before each complete
// transaction attempt. Production wiring uses it for structured logs/metrics.
type AttemptObserver func(context.Context, int)

type retryDelay func(failedAttempt int) time.Duration

// RetryingTransactionRunner retries the complete transaction callback only for
// PostgreSQL serialization failures and deadlocks.
type RetryingTransactionRunner[T any] struct {
	inner     transactionExecutor[T]
	onAttempt AttemptObserver
	delay     retryDelay
}

func NewRetryingTransactionRunner[T any](inner transactionExecutor[T], observer AttemptObserver) *RetryingTransactionRunner[T] {
	return &RetryingTransactionRunner[T]{inner: inner, onAttempt: observer, delay: fullJitterDelay}
}

func (r *RetryingTransactionRunner[T]) WithinTransaction(ctx context.Context, fn func(context.Context, T) error) error {
	if ctx == nil {
		return errors.New("retry context is nil")
	}
	if fn == nil {
		return errors.New("retry callback is nil")
	}
	if r == nil || r.inner == nil {
		return errors.New("transaction executor is nil")
	}
	for attempt := 1; attempt <= MaxTransactionAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if r.onAttempt != nil {
			r.onAttempt(ctx, attempt)
		}
		err := r.inner.WithinTransaction(ctx, fn)
		if err == nil || !IsRetryableTransactionError(err) || attempt == MaxTransactionAttempts {
			return err
		}
		if err := waitForRetry(ctx, r.delay(attempt)); err != nil {
			return err
		}
	}
	return nil
}

func IsRetryableTransactionError(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}
	return postgresError.Code == "40001" || postgresError.Code == "40P01"
}

func fullJitterDelay(failedAttempt int) time.Duration {
	limit := retryBaseDelay
	for i := 1; i < failedAttempt && limit < retryDelayCap; i++ {
		limit *= 2
	}
	if limit > retryDelayCap {
		limit = retryDelayCap
	}
	return time.Duration(rand.Int64N(int64(limit) + 1))
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
