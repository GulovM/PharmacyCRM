package postgres

import (
	"errors"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/locking"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/jackc/pgx/v5"
)

// CanonicalLockRepository deliberately requires pgx.Tx. This prevents its
// FOR UPDATE methods from silently running in auto-commit mode.
type CanonicalLockRepository struct{ tx pgx.Tx }

func NewCanonicalLockRepository(tx pgx.Tx) (*CanonicalLockRepository, error) {
	if tx == nil {
		return nil, errors.Join(locking.ErrInvalidLockPlan, apperror.ErrInvalidArgument)
	}
	return &CanonicalLockRepository{tx: tx}, nil
}

var _ locking.Repository = (*CanonicalLockRepository)(nil)
