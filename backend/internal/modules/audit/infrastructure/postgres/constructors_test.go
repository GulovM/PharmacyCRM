package postgres

import (
	"context"
	"errors"
	"testing"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
)

func TestTransactionalAuditRepositoryRejectsMissingExecutor(t *testing.T) {
	repository, err := NewTransactionalAuditRepository(nil)
	if repository != nil || !errors.Is(err, database.ErrDependencyMissing) {
		t.Fatalf("repository=%v err=%v", repository, err)
	}
	var nilRepository *TransactionalAuditRepository
	if err := nilRepository.Append(context.Background(), audit.Event{}); !errors.Is(err, database.ErrDependencyMissing) {
		t.Fatalf("nil receiver error=%v", err)
	}
}
