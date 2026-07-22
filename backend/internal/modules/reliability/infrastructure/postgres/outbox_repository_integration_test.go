package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testOutboxRepository(tx pgx.Tx) *TransactionalOutboxRepository {
	return NewTransactionalOutboxRepository(database.WrapPGXTransaction(tx))
}

func withinOutboxTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(*TransactionalOutboxRepository) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(testOutboxRepository(tx)); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func TestOutboxLeaseProtocolIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock(92845001)"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(92845001)")
		lockConn.Release()
	}()
	if _, err := pool.Exec(ctx, "DELETE FROM outbox_events WHERE event_name IN ('test.outbox','test.replay')"); err != nil {
		t.Fatal(err)
	}

	aggregateID := uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM outbox_events WHERE aggregate_id = $1", aggregateID)
	})
	appendEvent := func(maxAttempts int16) uuid.UUID {
		id := uuid.New()
		event := outbox.Event{
			ID: id, EventKey: outbox.EventKey{Name: "test.outbox", Version: 1},
			AggregateType: "test", AggregateID: aggregateID, PartitionKey: aggregateID.String(),
			DeduplicationKey: id.String(), Payload: []byte(`{"value":1}`),
			OccurredAt: time.Now(), MaxAttempts: maxAttempts,
		}
		if err := withinOutboxTransaction(ctx, pool, func(repository *TransactionalOutboxRepository) error {
			writer := outbox.NewWriter(repository, map[outbox.EventKey]outbox.PayloadValidator{
				{Name: "test.outbox", Version: 1}: outbox.PayloadValidatorFunc(func(json.RawMessage) error { return nil }),
			})
			return writer.Append(ctx, event)
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	claim := func(now time.Time, owner string, limit int) []outbox.Lease {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		leases, err := testOutboxRepository(tx).ClaimBatch(ctx, outbox.ClaimRequest{
			Owner: owner, Limit: limit, LeaseDuration: 30 * time.Second, Now: now,
			Protocols: []outbox.EventKey{{Name: "test.outbox", Version: 1}},
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return leases
	}

	// Crash before the side effect: the expired lease is reclaimed with a new
	// fencing token and generation. An acknowledgement from the stale owner is rejected.
	id := appendEvent(3)
	now := time.Now()
	first := claim(now, "worker-a", 1)[0]
	second := claim(now.Add(time.Minute), "worker-b", 1)[0]
	if first.ID != id || second.ID != id || second.Attempt != 2 || second.Generation <= first.Generation || second.Token == first.Token {
		t.Fatalf("invalid reclaim: first=%#v second=%#v", first, second)
	}
	if err := withinOutboxTransaction(ctx, pool, func(repository *TransactionalOutboxRepository) error {
		return repository.MarkProcessed(ctx, first, now.Add(5*time.Second))
	}); !errors.Is(err, outbox.ErrStaleLease) {
		t.Fatalf("stale owner accepted: %v", err)
	}
	wrongOwner := second
	wrongOwner.Owner = "worker-c"
	if err := withinOutboxTransaction(ctx, pool, func(repository *TransactionalOutboxRepository) error {
		return repository.MarkProcessed(ctx, wrongOwner, now.Add(time.Minute+time.Second))
	}); !errors.Is(err, outbox.ErrStaleLease) {
		t.Fatalf("wrong lease owner accepted: %v", err)
	}
	if err := withinOutboxTransaction(ctx, pool, func(repository *TransactionalOutboxRepository) error {
		return repository.MarkProcessed(ctx, second, now.Add(time.Minute+time.Second))
	}); err != nil {
		t.Fatal(err)
	}

	// Crash after the side effect but before acknowledgement causes duplicate
	// delivery. The handler's deduplication key keeps the effect idempotent.
	duplicateID := appendEvent(3)
	initialDelivery := claim(now.Add(2*time.Minute), "worker-a", 1)[0]
	effects := map[string]int{}
	apply := func(event outbox.Event) {
		if effects[event.DeduplicationKey] == 0 {
			effects[event.DeduplicationKey]++
		}
	}
	apply(initialDelivery.Event)
	duplicateDelivery := claim(initialDelivery.ExpiresAt.Add(time.Second), "worker-b", 1)[0]
	apply(duplicateDelivery.Event)
	if duplicateDelivery.ID != duplicateID || effects[duplicateID.String()] != 1 {
		t.Fatalf("duplicate delivery was not idempotent: lease=%#v effects=%#v", duplicateDelivery, effects)
	}
	if err := withinOutboxTransaction(ctx, pool, func(repository *TransactionalOutboxRepository) error {
		return repository.MarkProcessed(ctx, duplicateDelivery, duplicateDelivery.ExpiresAt.Add(-time.Second))
	}); err != nil {
		t.Fatal(err)
	}

	// Poison events are dead-lettered, then can be replayed manually.
	poisonID := appendEvent(1)
	poisonClaimedAt := now.Add(10 * time.Minute)
	poison := claim(poisonClaimedAt, "worker-a", 1)[0]
	if poison.ID != poisonID {
		t.Fatalf("claimed %s, want poison %s", poison.ID, poisonID)
	}
	failedAt := poisonClaimedAt.Add(time.Second)
	if err := withinOutboxTransaction(ctx, pool, func(repository *TransactionalOutboxRepository) error {
		return repository.MarkFailed(ctx, poison, outbox.Failure{Code: "POISON_EVENT", Retryable: false}, failedAt, failedAt)
	}); err != nil {
		t.Fatal(err)
	}
	if err := withinOutboxTransaction(ctx, pool, func(repository *TransactionalOutboxRepository) error {
		return repository.ReplayDeadLetter(ctx, poisonID, failedAt.Add(time.Second))
	}); err != nil {
		t.Fatal(err)
	}
	replayed := claim(failedAt.Add(2*time.Second), "worker-b", 1)[0]
	if replayed.ID != poisonID || replayed.Attempt != 1 {
		t.Fatalf("invalid replay lease: %#v", replayed)
	}

	// Two transactions claiming concurrently never own the same row.
	raceID := appendEvent(2)
	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	leasingAt := now.Add(20 * time.Minute)
	workerOne, err := testOutboxRepository(tx1).ClaimBatch(ctx, outbox.ClaimRequest{Owner: "worker-1", Limit: 1, LeaseDuration: time.Minute, Now: leasingAt, Protocols: []outbox.EventKey{{Name: "test.outbox", Version: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workerTwo, err := testOutboxRepository(tx2).ClaimBatch(ctx, outbox.ClaimRequest{Owner: "worker-2", Limit: 1, LeaseDuration: time.Minute, Now: leasingAt, Protocols: []outbox.EventKey{{Name: "test.outbox", Version: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if len(workerOne) != 1 || workerOne[0].ID != raceID || len(workerTwo) != 0 {
		t.Fatalf("race claim worker1=%#v worker2=%#v", workerOne, workerTwo)
	}
}
