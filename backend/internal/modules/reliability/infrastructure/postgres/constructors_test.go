package postgres

import (
	"errors"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
)

func TestConstructorsRejectMissingDependencies(t *testing.T) {
	tests := []struct {
		name string
		call func() error
	}{
		{"idempotency repository", func() error { _, err := NewTransactionalIdempotencyRepository(nil); return err }},
		{"outbox repository", func() error { _, err := NewTransactionalOutboxRepository(nil); return err }},
		{"retention repository", func() error { _, err := NewTransactionalOutboxRetentionRepository(nil); return err }},
		{"outbox transactor", func() error { _, err := NewOutboxTransactor(nil, nil); return err }},
		{"retention transactor", func() error { _, err := NewOutboxRetentionTransactor(nil, nil); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, database.ErrDependencyMissing) {
				t.Fatalf("expected ErrDependencyMissing, got %v", err)
			}
		})
	}
}
