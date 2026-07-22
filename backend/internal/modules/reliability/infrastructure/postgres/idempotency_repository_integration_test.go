package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdempotencyRepositoryIntegration(t *testing.T) {
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
	if _, err := pool.Exec(ctx, "INSERT INTO users (id, login, password_hash, display_name) VALUES ($1, $2, 'hash', 'Idempotency Test')", actorID, "idem-"+actorID.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM idempotency_records WHERE actor_user_id = $1; DELETE FROM users WHERE id = $1", actorID)
	})

	claim := idempotency.Claim{Identity: idempotency.Identity{ActorID: actorID, Operation: "test.complete", Key: "same-key"}, Fingerprint: idempotency.NewFingerprint([]byte(`{"value":1}`)), ExpiresAt: time.Now().Add(time.Hour)}
	var first idempotency.ClaimResult
	withinIdempotencyTransaction(t, ctx, pool, func(service *idempotency.Service) error {
		var err error
		first, err = service.Claim(ctx, claim)
		if err != nil {
			return err
		}
		return service.Complete(ctx, idempotency.Completion{RecordID: first.RecordID, Result: idempotency.StoredResult{ResponseStatus: 201, ResponseBody: json.RawMessage(`{"id":"created"}`)}})
	})
	if first.State != idempotency.Claimed {
		t.Fatalf("unexpected first state: %s", first.State)
	}

	withinIdempotencyTransaction(t, ctx, pool, func(service *idempotency.Service) error {
		replay, err := service.Claim(ctx, claim)
		if err != nil {
			return err
		}
		if replay.State != idempotency.ReplayAvailable || replay.Replay == nil || replay.Replay.ResponseStatus != 201 {
			t.Fatalf("invalid replay: %#v", replay)
		}
		return nil
	})

	conflict := claim
	conflict.Fingerprint = idempotency.NewFingerprint([]byte(`{"value":2}`))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = idempotency.NewService(NewIdempotencyRepository(tx)).Claim(ctx, conflict)
	_ = tx.Rollback(ctx)
	if !errors.Is(err, idempotency.ErrKeyReused) {
		t.Fatalf("expected fingerprint conflict, got %v", err)
	}

	testRetryableReclaim(t, ctx, pool, actorID)
	testConcurrentClaimReplay(t, ctx, pool, actorID)
}

func testRetryableReclaim(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actorID uuid.UUID) {
	t.Helper()
	claim := idempotency.Claim{Identity: idempotency.Identity{ActorID: actorID, Operation: "test.retryable", Key: "retryable-key"}, Fingerprint: idempotency.NewFingerprint([]byte(`{"retry":true}`)), ExpiresAt: time.Now().Add(time.Hour)}
	var recordID idempotency.RecordID
	withinIdempotencyTransaction(t, ctx, pool, func(service *idempotency.Service) error {
		result, err := service.Claim(ctx, claim)
		if err != nil {
			return err
		}
		recordID = result.RecordID
		return service.MarkRetryableFailure(ctx, recordID, true)
	})
	withinIdempotencyTransaction(t, ctx, pool, func(service *idempotency.Service) error {
		result, err := service.Claim(ctx, claim)
		if err != nil {
			return err
		}
		if result.State != idempotency.Claimed || result.RecordID != recordID {
			t.Fatalf("retryable record was not reclaimed: %#v", result)
		}
		return service.Complete(ctx, idempotency.Completion{RecordID: recordID, Result: idempotency.StoredResult{ResponseStatus: 200, ResponseBody: json.RawMessage(`{"retry":"completed"}`)}})
	})
}

func withinIdempotencyTransaction(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fn func(*idempotency.Service) error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := fn(idempotency.NewService(NewIdempotencyRepository(tx))); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func testConcurrentClaimReplay(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actorID uuid.UUID) {
	t.Helper()
	claim := idempotency.Claim{Identity: idempotency.Identity{ActorID: actorID, Operation: "test.concurrent", Key: "race-key"}, Fingerprint: idempotency.NewFingerprint([]byte(`{"race":true}`)), ExpiresAt: time.Now().Add(time.Hour)}
	tx1, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := idempotency.NewService(NewIdempotencyRepository(tx1)).Claim(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan idempotency.ClaimResult, 1)
	errorsCh := make(chan error, 1)
	go func() {
		tx2, err := pool.Begin(ctx)
		if err != nil {
			errorsCh <- err
			return
		}
		defer tx2.Rollback(ctx)
		second, err := idempotency.NewService(NewIdempotencyRepository(tx2)).Claim(ctx, claim)
		if err == nil {
			err = tx2.Commit(ctx)
		}
		if err != nil {
			errorsCh <- err
			return
		}
		result <- second
	}()

	time.Sleep(50 * time.Millisecond)
	service1 := idempotency.NewService(NewIdempotencyRepository(tx1))
	if err := service1.Complete(ctx, idempotency.Completion{RecordID: first.RecordID, Result: idempotency.StoredResult{ResponseStatus: 200, ResponseBody: json.RawMessage(`{"race":"winner"}`)}}); err != nil {
		t.Fatal(err)
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errorsCh:
		t.Fatal(err)
	case second := <-result:
		if second.State != idempotency.ReplayAvailable || second.Replay == nil {
			t.Fatalf("concurrent claim did not replay: %#v", second)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent claim timed out")
	}
}
