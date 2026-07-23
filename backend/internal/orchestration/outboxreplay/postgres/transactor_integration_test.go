package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	"github.com/GulovM/PharmacyCRM/backend/internal/orchestration/outboxreplay"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManualReplayRevalidatesAdminAndIsIdempotentIntegration(t *testing.T) {
	ctx := context.Background()
	dsn := postgrestest.DSN(t)
	rawPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rawPool.Close)

	actorID, sessionID, eventID, deniedEventID, auditID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	var adminRoleID uuid.UUID
	if err := rawPool.QueryRow(ctx, "SELECT id FROM roles WHERE code='ADMIN'").Scan(&adminRoleID); err != nil {
		t.Fatal(err)
	}
	roleAssignmentID := uuid.New()
	t.Cleanup(func() {
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM audit_events WHERE actor_user_id=$1", actorID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM idempotency_records WHERE actor_user_id=$1", actorID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM outbox_events WHERE id IN ($1,$2)", eventID, deniedEventID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM user_sessions WHERE id=$1", sessionID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM user_roles WHERE id=$1", roleAssignmentID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", actorID)
	})
	if _, err := rawPool.Exec(ctx, "INSERT INTO users(id,login,password_hash,display_name) VALUES($1,$2,'hash','Replay Administrator')", actorID, "replay-"+actorID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := rawPool.Exec(ctx, "INSERT INTO user_roles(id,user_id,role_id,assigned_by_user_id) VALUES($1,$2,$3,$2)", roleAssignmentID, actorID, adminRoleID); err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(time.Hour)
	if _, err := rawPool.Exec(ctx, `INSERT INTO user_sessions(
		id,user_id,refresh_token_hash,token_family_id,generation,expires_at,idle_expires_at,
		absolute_expires_at,authentication_method,mfa_level
	) VALUES($1,$2,$3,$4,1,$5,$5,$5,'PASSWORD_MFA','TOTP')`, sessionID, actorID, []byte(uuid.NewString()), uuid.New(), expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := rawPool.Exec(ctx, `INSERT INTO outbox_events(
		id,event_name,aggregate_type,aggregate_id,partition_key,deduplication_key,payload,
		status,max_attempts,occurred_at,dead_lettered_at
	) VALUES
		($1,'test.replay','test',$1,($1::uuid)::text,($1::uuid)::text,'{}','DEAD_LETTER',1,now(),now()),
		($2,'test.replay','test',$2,($2::uuid)::text,($2::uuid)::text,'{}','DEAD_LETTER',1,now(),now())`, eventID, deniedEventID); err != nil {
		t.Fatal(err)
	}

	pool, err := database.NewAPI(ctx, config.APIPostgresConfig{DSN: dsn, PoolConfig: config.PoolConfig{
		MinConnections: 1, MaxConnections: 2, AcquireTimeout: time.Second,
		MaxConnectionLife: time.Minute, MaxConnectionIdle: time.Minute, HealthCheckPeriod: time.Minute,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	transactor, err := NewTransactor(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := outboxreplay.NewService(transactor)
	command := outboxreplay.Command{
		EventID: eventID, AuditEventID: auditID, ActorUserID: actorID, ActorSessionID: sessionID,
		Reason: "operator approved replay", IdempotencyKey: "manual-replay-" + eventID.String(),
		Fingerprint:          idempotency.NewFingerprint([]byte(eventID.String() + "|operator approved replay")),
		IdempotencyExpiresAt: time.Now().Add(time.Hour), OccurredAt: time.Now(),
	}
	first, err := service.Replay(ctx, command)
	if err != nil || first.IdempotencyReplayed {
		t.Fatalf("first replay=%#v err=%v", first, err)
	}
	second, err := service.Replay(ctx, command)
	if err != nil || !second.IdempotencyReplayed {
		t.Fatalf("idempotent replay=%#v err=%v", second, err)
	}

	var status, idempotencyStatus string
	var auditCount int
	if err := rawPool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE id=$1", eventID).Scan(&status); err != nil || status != "PENDING" {
		t.Fatalf("event status=%q err=%v", status, err)
	}
	if err := rawPool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE id=$1", auditID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}
	if err := rawPool.QueryRow(ctx, "SELECT status FROM idempotency_records WHERE actor_user_id=$1 AND operation=$2 AND idempotency_key=$3", actorID, outboxreplay.Operation, command.IdempotencyKey).Scan(&idempotencyStatus); err != nil || idempotencyStatus != "COMPLETED" {
		t.Fatalf("idempotency status=%q err=%v", idempotencyStatus, err)
	}

	if _, err := rawPool.Exec(ctx, "UPDATE user_sessions SET revoked_at=now(), revoke_reason='TEST_REVOKE' WHERE id=$1", sessionID); err != nil {
		t.Fatal(err)
	}
	denied := command
	denied.EventID = deniedEventID
	denied.AuditEventID = uuid.New()
	denied.IdempotencyKey = "manual-replay-" + deniedEventID.String()
	denied.Fingerprint = idempotency.NewFingerprint([]byte(deniedEventID.String()))
	if _, err := service.Replay(ctx, denied); !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("expected forbidden replay, got %v", err)
	}
	if err := rawPool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE id=$1", deniedEventID).Scan(&status); err != nil || status != "DEAD_LETTER" {
		t.Fatalf("denied event status=%q err=%v", status, err)
	}
}
