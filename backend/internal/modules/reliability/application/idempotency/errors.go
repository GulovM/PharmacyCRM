package idempotency

import (
	"errors"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
)

var (
	ErrKeyRequired            = errors.New("idempotency key required")
	ErrKeyReused              = errors.New("idempotency key reused")
	ErrConcurrentModification = errors.New("concurrent modification")
	ErrInvalidState           = errors.New("invalid idempotency state")
	ErrDependencyMissing      = errors.New("required dependency is missing")
)

func keyRequiredError() error {
	return errors.Join(ErrKeyRequired, &apperror.Typed{Category: apperror.ErrInvalidArgument, Code: "IDEMPOTENCY_KEY_REQUIRED"})
}

func keyReusedError() error {
	return errors.Join(ErrKeyReused, &apperror.Typed{Category: apperror.ErrConflict, Code: "IDEMPOTENCY_KEY_REUSED"})
}

func concurrentModificationError() error {
	return errors.Join(ErrConcurrentModification, &apperror.Typed{Category: apperror.ErrConflict, Code: "CONCURRENT_MODIFICATION"})
}
