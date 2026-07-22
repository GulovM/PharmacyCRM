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
	if _, err := api.Exec(ctx, `UPDATE idempotency_records SET status='FAILED_RETRYABLE',completed_at=now() WHERE id=$1`, recordID); err != nil {
		t.Fatalf("API retryable failure update rejected: %v", err)
	}
	if _, err := api.Exec(ctx, `UPDATE idempotency_records SET status='IN_PROGRESS',response_status=NULL,response_body=NULL,resource_type=NULL,resource_id=NULL,completed_at=NULL,expires_at=now()+interval '3 hours' WHERE id=$1`, recordID); err != nil {
		t.Fatalf("API retry reclaim update rejected: %v", err)
	}
	for name, statement := range map[string]string{
		"actor":           `UPDATE idempotency_records SET actor_user_id=gen_random_uuid() WHERE id=$1`,
		"pharmacy":        `UPDATE idempotency_records SET pharmacy_id=gen_random_uuid() WHERE id=$1`,
		"operation":       `UPDATE idempotency_records SET operation='forbidden' WHERE id=$1`,
		"idempotency key": `UPDATE idempotency_records SET idempotency_key='forbidden' WHERE id=$1`,
		"scope":           `UPDATE idempotency_records SET scope_key=scope_key WHERE id=$1`,
		"request hash":    `UPDATE idempotency_records SET request_hash=decode('01','hex') WHERE id=$1`,
		"created at":      `UPDATE idempotency_records SET created_at=now() WHERE id=$1`,
	} {
		t.Run("denies "+name, func(t *testing.T) {
			assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, statement, recordID); return err })
		})
	}
	assertPrivilegeDenied(t, func() error {
		_, err := api.Exec(ctx, `DELETE FROM idempotency_records WHERE id=$1`, recordID)
		return err
	})
	assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, `TRUNCATE idempotency_records`); return err })
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
	assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, `DELETE FROM outbox_events WHERE id=$1`, eventID); return err })
	assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, `TRUNCATE outbox_events`); return err })
}

func assertPrivilegeDenied(t testing.TB, call func() error) {
	t.Helper()
	err := call()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
		t.Fatalf("expected SQLSTATE 42501, got %v", err)
	}
}
