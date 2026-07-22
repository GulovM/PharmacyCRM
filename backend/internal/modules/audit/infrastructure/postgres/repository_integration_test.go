package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuditWriterIntegration(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	actorID, otherActorID := uuid.New(), uuid.New()
	actorSessionID, otherSessionID := uuid.New(), uuid.New()
	login := "audit-" + actorID.String()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, login, password_hash, display_name) VALUES
		($1,$2,'hash','Before'),($3,$4,'hash','Other Actor')`, actorID, login, otherActorID, "audit-"+otherActorID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_sessions(id,user_id,refresh_token_hash,token_family_id,expires_at) VALUES
		($1,$2,convert_to(gen_random_uuid()::text,'UTF8'),gen_random_uuid(),now()+interval '1 day'),
		($3,$4,convert_to(gen_random_uuid()::text,'UTF8'),gen_random_uuid(),now()+interval '1 day')`, actorSessionID, actorID, otherSessionID, otherActorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE actor_user_id IN ($1,$2)", actorID, otherActorID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM user_sessions WHERE id IN ($1,$2)", actorSessionID, otherSessionID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id IN ($1,$2)", actorID, otherActorID)
	})
	policy := audit.MetadataPolicy{"test.user.changed": {"reason": audit.MetadataString}}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writer := audit.NewWriter(NewTransactionalAuditRepository(database.WrapPGXTransaction(tx)), policy)
	event := audit.Event{ID: uuid.New(), OccurredAt: time.Now(), ActorUserID: &actorID, ActorSessionID: &actorSessionID, ActorType: audit.ActorUser, Action: "test.user.changed", ObjectType: "user", ObjectID: &actorID, Result: audit.ResultSuccess, Metadata: audit.Metadata{"reason": "integration"}}
	if err := writer.Append(ctx, event); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	withoutSession := event
	withoutSession.ID = uuid.New()
	withoutSession.ActorSessionID = nil
	if err := writer.Append(ctx, withoutSession); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("user audit without session rejected: %v", err)
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
	failing := event
	failing.ID = uuid.New()
	failing.ActorSessionID = &otherSessionID
	if err := audit.NewWriter(NewTransactionalAuditRepository(database.WrapPGXTransaction(tx)), policy).Append(ctx, failing); err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("expected mismatched audit session failure")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
			_ = tx.Rollback(ctx)
			t.Fatalf("expected foreign key violation, got %v", err)
		}
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

	runtimePool, err := pgxpool.New(ctx, postgrestest.RuntimeDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer runtimePool.Close()
	_, err = runtimePool.Exec(ctx, `INSERT INTO audit_events(
		id,occurred_at,actor_user_id,actor_session_id,actor_type,action,object_type,result
	) VALUES($1,now(),$2,$3,'USER','test.direct','user','FAILURE')`, uuid.New(), actorID, otherSessionID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("runtime direct SQL bypassed audit ownership invariant: %v", err)
	}
}
