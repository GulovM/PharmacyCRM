package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuditWriterIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	actorID := uuid.New()
	login := "audit-" + actorID.String()
	if _, err := pool.Exec(ctx, "INSERT INTO users (id, login, password_hash, display_name) VALUES ($1,$2,'hash','Before')", actorID, login); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE actor_user_id = $1; DELETE FROM users WHERE id = $1", actorID)
	})
	policy := audit.MetadataPolicy{"test.user.changed": {"reason": audit.MetadataString}}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writer := audit.NewWriter(NewRepository(tx), policy)
	event := audit.Event{ID: uuid.New(), OccurredAt: time.Now(), ActorUserID: &actorID, ActorType: audit.ActorUser, Action: "test.user.changed", ObjectType: "user", ObjectID: &actorID, Result: audit.ResultSuccess, Metadata: audit.Metadata{"reason": "integration"}}
	if err := writer.Append(ctx, event); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var reason string
	if err := pool.QueryRow(ctx, "SELECT metadata->>'reason' FROM audit_events WHERE id = $1", event.ID).Scan(&reason); err != nil || reason != "integration" {
		t.Fatalf("reason=%q err=%v", reason, err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE users SET display_name = 'Should Roll Back' WHERE id = $1", actorID); err != nil {
		t.Fatal(err)
	}
	missingSession := uuid.New()
	failing := event
	failing.ID = uuid.New()
	failing.ActorSessionID = &missingSession
	if err := audit.NewWriter(NewRepository(tx), policy).Append(ctx, failing); err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("expected mandatory audit insert failure")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	var displayName string
	if err := pool.QueryRow(ctx, "SELECT display_name FROM users WHERE id = $1", actorID).Scan(&displayName); err != nil {
		t.Fatal(err)
	}
	if displayName != "Before" {
		t.Fatalf("business effect survived audit failure: %q", displayName)
	}
}
