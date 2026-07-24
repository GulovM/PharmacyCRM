package migration

import (
	"testing"

	embeddedmigrations "github.com/GulovM/PharmacyCRM/backend/migrations"
)

func TestEmbeddedMigrationsLoad(t *testing.T) {
	items, err := Load(embeddedmigrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 25 || items[0].Version != 1 || items[len(items)-1].Version != 25 {
		t.Fatalf("unexpected embedded migrations: %#v", items)
	}
	seen := make(map[int64]struct{}, len(items))
	for _, item := range items {
		if item.VerificationSQL == "" {
			t.Fatalf("migration %d has no verification query", item.Version)
		}
		if _, duplicate := seen[item.Version]; duplicate {
			t.Fatalf("duplicate migration version %d", item.Version)
		}
		seen[item.Version] = struct{}{}
	}
}
