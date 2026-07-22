package postgres

import (
	"testing"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
)

func mustAuditWriter(t testing.TB, repository audit.Repository, policy audit.MetadataPolicy) *audit.Writer {
	t.Helper()
	writer, err := audit.NewWriter(repository, policy)
	if err != nil {
		t.Fatal(err)
	}
	return writer
}

func mustIdempotencyService(t testing.TB, repository idempotency.Repository) *idempotency.Service {
	t.Helper()
	service, err := idempotency.NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func mustOutboxWriter(t testing.TB, repository outbox.Repository, validators map[outbox.EventKey]outbox.PayloadValidator) *outbox.Writer {
	t.Helper()
	writer, err := outbox.NewWriter(repository, validators)
	if err != nil {
		t.Fatal(err)
	}
	return writer
}
