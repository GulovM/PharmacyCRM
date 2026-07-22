package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/orchestration/outboxreplay"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManualReplayAndAuditCommitAtomicallyIntegration(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	rawPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rawPool.Close)

	actorID, eventID, failedEventID, auditID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() {
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM audit_events WHERE id = $1", auditID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM outbox_events WHERE id IN ($1,$2)", eventID, failedEventID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", actorID)
	})
	if _, err := rawPool.Exec(ctx, "INSERT INTO users (id, login, password_hash, display_name) VALUES ($1,$2,'hash','Replay Operator')", actorID, "replay-"+actorID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := rawPool.Exec(ctx, `INSERT INTO outbox_events (
			id, event_name, aggregate_type, aggregate_id, partition_key,
			deduplication_key, payload, status, max_attempts, occurred_at, dead_lettered_at
		) VALUES
			($1::uuid,'test.replay','test',$1::uuid,($1::uuid)::text,($1::uuid)::text,'{}','DEAD_LETTER',1,now(),now()),
			($2::uuid,'test.replay','test',$2::uuid,($2::uuid)::text,($2::uuid)::text,'{}','DEAD_LETTER',1,now(),now())`,
		eventID, failedEventID); err != nil {
		t.Fatal(err)
	}

	pool, err := database.NewAPI(ctx, config.APIPostgresConfig{
		DSN: dsn,
		PoolConfig: config.PoolConfig{
			MinConnections: 1, MaxConnections: 2, AcquireTimeout: time.Second,
			MaxConnectionLife: time.Minute, MaxConnectionIdle: time.Minute, HealthCheckPeriod: time.Minute,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	transactor, err := NewTransactor(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := outboxreplay.NewService(transactor)
	if err := service.Replay(ctx, outboxreplay.Command{
		EventID: eventID, AuditEventID: auditID, ActorUserID: actorID,
		Reason: "operator approved replay", OccurredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	var status, action, reason string
	if err := rawPool.QueryRow(ctx, `
		SELECT event.status, audit.action, audit.metadata->>'reason'
		FROM outbox_events event
		JOIN audit_events audit ON audit.object_id = event.id
		WHERE event.id = $1 AND audit.id = $2`, eventID, auditID).Scan(&status, &action, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "PENDING" || action != outboxreplay.AuditAction || reason != "operator approved replay" {
		t.Fatalf("status=%q action=%q reason=%q", status, action, reason)
	}

	// The replay is rolled back if its mandatory audit insert cannot commit.
	if err := service.Replay(ctx, outboxreplay.Command{
		EventID: failedEventID, AuditEventID: uuid.New(), ActorUserID: uuid.New(),
		Reason: "actor does not exist", OccurredAt: time.Now(),
	}); err == nil {
		t.Fatal("expected audit foreign-key failure")
	}
	if err := rawPool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE id = $1", failedEventID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "DEAD_LETTER" {
		t.Fatalf("replay survived audit failure: %q", status)
	}
}
