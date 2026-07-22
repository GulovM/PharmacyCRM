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
	owner := openCapabilityPool(t, ctx, postgrestest.DSN(t))
	api := openCapabilityPool(t, ctx, postgrestest.RuntimeDSN(t))
	worker := openCapabilityPool(t, ctx, postgrestest.WorkerRuntimeDSN(t))

	actorID := uuid.New()
	if _, err := owner.Exec(ctx, `INSERT INTO users(id,login,password_hash,display_name) VALUES($1,$2,'hash','API Capability Test')`, actorID, "api-capability-"+actorID.String()); err != nil {
		t.Fatal(err)
	}

	completedRecordID, retryRecordID := uuid.New(), uuid.New()
	eventIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	t.Cleanup(func() {
		_, _ = owner.Exec(context.Background(), `DELETE FROM idempotency_records WHERE id IN ($1,$2)`, completedRecordID, retryRecordID)
		_, _ = owner.Exec(context.Background(), `DELETE FROM outbox_events WHERE id = ANY($1)`, eventIDs)
		_, _ = owner.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, actorID)
	})

	insertIdempotency := func(id uuid.UUID, key string) {
		t.Helper()
		if _, err := api.Exec(ctx, `INSERT INTO idempotency_records(id,actor_user_id,operation,idempotency_key,request_hash,expires_at) VALUES($1,$2,'test.api',$3,decode('00','hex'),now()+interval '1 hour')`, id, actorID, key); err != nil {
			t.Fatalf("API idempotency insert rejected: %v", err)
		}
	}
	insertIdempotency(completedRecordID, "completed")
	insertIdempotency(retryRecordID, "retry")

	if _, err := api.Exec(ctx, `UPDATE idempotency_records SET status='COMPLETED',response_status=200,response_body='{}',completed_at=now(),expires_at=now()+interval '2 hours' WHERE id=$1`, completedRecordID); err != nil {
		t.Fatalf("API completion update rejected: %v", err)
	}
	if _, err := api.Exec(ctx, `UPDATE idempotency_records SET status='FAILED_RETRYABLE',completed_at=now() WHERE id=$1 AND status='IN_PROGRESS'`, retryRecordID); err != nil {
		t.Fatalf("API retryable failure update rejected: %v", err)
	}
	if _, err := api.Exec(ctx, `UPDATE idempotency_records SET status='IN_PROGRESS',response_status=NULL,response_body=NULL,resource_type=NULL,resource_id=NULL,completed_at=NULL,expires_at=now()+interval '3 hours' WHERE id=$1 AND status='FAILED_RETRYABLE'`, retryRecordID); err != nil {
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
			assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, statement, retryRecordID); return err })
		})
	}
	assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, `DELETE FROM idempotency_records WHERE id=$1`, retryRecordID); return err })
	assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, `TRUNCATE idempotency_records`); return err })

	statuses := []string{"DEAD_LETTER", "PENDING", "PROCESSING", "PROCESSED"}
	for index, status := range statuses {
		id := eventIDs[index]
		statement := `INSERT INTO outbox_events(id,event_name,event_version,aggregate_type,aggregate_id,partition_key,deduplication_key,payload,headers,status,attempt_count,max_attempts,available_at,lease_token,lease_generation,leased_by,lease_expires_at,last_error_code,last_error_at,occurred_at,processed_at,dead_lettered_at)
			VALUES($1,'test.api',2,'test',$1,$2,$3,'{"value":1}','{"source":"test"}',$4,3,8,now()-interval '1 hour',$5,7,$6,$7,'TEST_FAILURE',now()-interval '1 minute',now()-interval '2 hours',$8,$9)`
		var leaseToken any
		var leasedBy any
		var leaseExpiresAt any
		var processedAt any
		var deadLetteredAt any
		switch status {
		case "PROCESSING":
			leaseToken, leasedBy, leaseExpiresAt = uuid.New(), "worker-test", time.Now().Add(time.Minute)
		case "PROCESSED":
			processedAt = time.Now()
		case "DEAD_LETTER":
			deadLetteredAt = time.Now()
		}
		if _, err := owner.Exec(ctx, statement, id, id.String(), "dedupe-"+id.String(), status, leaseToken, leasedBy, leaseExpiresAt, processedAt, deadLetteredAt); err != nil {
			t.Fatalf("insert %s event: %v", status, err)
		}
	}

	requestedAvailableAt := time.Now().UTC().Truncate(time.Microsecond)
	var replayed bool
	if err := api.QueryRow(ctx, `SELECT public.replay_dead_letter_outbox_event($1,$2)`, eventIDs[0], requestedAvailableAt).Scan(&replayed); err != nil || !replayed {
		t.Fatalf("API capability replay=%t err=%v", replayed, err)
	}
	for _, id := range eventIDs[1:] {
		if err := api.QueryRow(ctx, `SELECT public.replay_dead_letter_outbox_event($1,now())`, id).Scan(&replayed); err != nil || replayed {
			t.Fatalf("non-dead-letter event %s replay=%t err=%v", id, replayed, err)
		}
	}
	if err := api.QueryRow(ctx, `SELECT public.replay_dead_letter_outbox_event($1,now())`, uuid.New()).Scan(&replayed); err != nil || replayed {
		t.Fatalf("unknown event replay=%t err=%v", replayed, err)
	}

	var status, eventName, aggregateType, partitionKey, deduplicationKey, payload, headers string
	var eventVersion int16
	var aggregateID uuid.UUID
	var attemptCount int16
	var availableAt time.Time
	var leaseToken *uuid.UUID
	var leasedBy, lastErrorCode *string
	var leaseExpiresAt, lastErrorAt, deadLetteredAt *time.Time
	if err := api.QueryRow(ctx, `SELECT status,event_name,event_version,aggregate_type,aggregate_id,partition_key,deduplication_key,payload::text,headers::text,attempt_count,available_at,lease_token,leased_by,lease_expires_at,last_error_code,last_error_at,dead_lettered_at FROM outbox_events WHERE id=$1`, eventIDs[0]).Scan(
		&status, &eventName, &eventVersion, &aggregateType, &aggregateID, &partitionKey, &deduplicationKey, &payload, &headers, &attemptCount, &availableAt, &leaseToken, &leasedBy, &leaseExpiresAt, &lastErrorCode, &lastErrorAt, &deadLetteredAt,
	); err != nil {
		t.Fatal(err)
	}
	if status != "PENDING" || eventName != "test.api" || eventVersion != 2 || aggregateType != "test" || aggregateID != eventIDs[0] || partitionKey != eventIDs[0].String() || deduplicationKey != "dedupe-"+eventIDs[0].String() || payload != `{"value": 1}` || headers != `{"source": "test"}` || attemptCount != 0 || !availableAt.Equal(requestedAvailableAt) || leaseToken != nil || leasedBy != nil || leaseExpiresAt != nil || lastErrorCode != nil || lastErrorAt != nil || deadLetteredAt != nil {
		t.Fatalf("unexpected replayed event state: status=%s payload=%s headers=%s attempts=%d available=%s", status, payload, headers, attemptCount, availableAt)
	}

	assertPrivilegeDenied(t, func() error { _, err := worker.Exec(ctx, `SELECT public.replay_dead_letter_outbox_event($1,now())`, eventIDs[0]); return err })
	var publicCanExecute bool
	if err := owner.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_proc procedure CROSS JOIN LATERAL aclexplode(COALESCE(procedure.proacl, acldefault('f', procedure.proowner))) privilege WHERE procedure.oid='public.replay_dead_letter_outbox_event(uuid,timestamptz)'::regprocedure AND privilege.grantee=0 AND privilege.privilege_type='EXECUTE')`).Scan(&publicCanExecute); err != nil || publicCanExecute {
		t.Fatalf("PUBLIC execute privilege=%t err=%v", publicCanExecute, err)
	}
	assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, `UPDATE outbox_events SET status='PENDING' WHERE id=$1`, eventIDs[0]); return err })
	assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, `DELETE FROM outbox_events WHERE id=$1`, eventIDs[0]); return err })
	assertPrivilegeDenied(t, func() error { _, err := api.Exec(ctx, `TRUNCATE outbox_events`); return err })
}

func openCapabilityPool(t testing.TB, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func assertPrivilegeDenied(t testing.TB, call func() error) {
	t.Helper()
	err := call()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
		t.Fatalf("expected SQLSTATE 42501, got %v", err)
	}
}
