package schema

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAPIRuntimeIdempotencyAndReplayCapabilitiesIntegration(t *testing.T) {
	ctx := context.Background()
	owner, err := pgxpool.New(ctx, postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	api, err := pgxpool.New(ctx, postgrestest.RuntimeDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	actorID, recordID, eventID := uuid.New(), uuid.New(), uuid.New()
	if _, err := owner.Exec(ctx, `INSERT INTO users(id,login,password_hash,display_name) VALUES($1,$2,'hash','API Capability Test')`, actorID, "api-capability-"+actorID.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = owner.Exec(context.Background(), `DELETE FROM idempotency_records WHERE id=$1`, recordID)
		_, _ = owner.Exec(context.Background(), `DELETE FROM outbox_events WHERE id=$1`, eventID)
		_, _ = owner.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, actorID)
	})
	if _, err := api.Exec(ctx, `INSERT INTO idempotency_records(id,actor_user_id,operation,idempotency_key,request_hash,expires_at) VALUES($1,$2,'test.api','lifecycle',decode('00','hex'),now()+interval '1 hour')`, recordID, actorID); err != nil {
		t.Fatalf("API idempotency insert rejected: %v", err)
	}
	if _, err := api.Exec(ctx, `UPDATE idempotency_records SET status='COMPLETED',response_status=200,response_body='{}',completed_at=now(),expires_at=now()+interval '2 hours' WHERE id=$1`, recordID); err != nil {
		t.Fatalf("API idempotency lifecycle update rejected: %v", err)
	}
	assertPrivilegeDenied(t, func() error {
		_, err := api.Exec(ctx, `UPDATE idempotency_records SET actor_user_id=$2 WHERE id=$1`, recordID, uuid.New())
		return err
	})
	assertPrivilegeDenied(t, func() error {
		_, err := api.Exec(ctx, `DELETE FROM idempotency_records WHERE id=$1`, recordID)
		return err
	})
	if _, err := owner.Exec(ctx, `INSERT INTO outbox_events(id,event_name,aggregate_type,aggregate_id,partition_key,deduplication_key,payload,status,occurred_at,dead_lettered_at) VALUES($1,'test.api','test',$1,$2,$3,'{}','DEAD_LETTER',now(),now())`, eventID, eventID.String(), eventID.String()); err != nil {
		t.Fatal(err)
	}
	var replayed bool
	if err := api.QueryRow(ctx, `SELECT public.replay_dead_letter_outbox_event($1,$2)`, eventID, time.Now()).Scan(&replayed); err != nil || !replayed {
		t.Fatalf("API capability replay=%t err=%v", replayed, err)
	}
	assertPrivilegeDenied(t, func() error {
		_, err := api.Exec(ctx, `UPDATE outbox_events SET status='PENDING' WHERE id=$1`, eventID)
		return err
	})
}

func assertPrivilegeDenied(t testing.TB, call func() error) {
	t.Helper()
	err := call()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
		t.Fatalf("expected SQLSTATE 42501, got %v", err)
	}
}
