package postgres

import (
	"errors"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
)

func TestTransactorRejectsMissingPool(t *testing.T) {
	transactor, err := NewTransactor(nil, nil)
	if transactor != nil || !errors.Is(err, database.ErrDependencyMissing) {
		t.Fatalf("transactor=%v err=%v", transactor, err)
	}
}
