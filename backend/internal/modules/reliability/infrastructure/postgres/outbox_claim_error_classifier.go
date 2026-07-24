package postgres

import (
	"context"
	"errors"
	"net"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgconn"
)

// OutboxClaimErrorClassifier permits retry only when replaying the complete
// claim transaction is known to be safe. Unknown repository and schema errors
// remain fatal so the worker cannot spin forever on corrupted state.
type OutboxClaimErrorClassifier struct{}

func (OutboxClaimErrorClassifier) IsTransientClaimError(err error) bool {
	if err == nil {
		return false
	}
	if database.IsRetryableTransactionError(err) || pgconn.SafeToRetry(err) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}
