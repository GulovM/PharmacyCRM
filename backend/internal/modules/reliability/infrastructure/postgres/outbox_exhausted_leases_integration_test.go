package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type outboxStateFixture struct {
	id           uuid.UUID
	status       string
	attempt      int16
	maxAttempts  int16
	leaseExpires *time.Time
}

func openOutboxTerminalizationPools(t *testing.T) (context.Context, *pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	ownerPool, err := pgxpool.New(ctx, postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ownerPool.Close)
	workerPool, err := pgxpool.New(ctx, postgrestest.WorkerRuntimeDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(workerPool.Close)
	return ctx, ownerPool, workerPool
}

func seedOutboxState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, aggregateID uuid.UUID, fixture outboxStateFixture) {
	t.Helper()
	var leaseToken any
	var leasedBy any
	var processedAt any
	var deadLetteredAt any
	if fixture.status == "PROCESSING" {
		leaseToken = uuid.New()
		leasedBy = "expired-worker"
	}
	now := time.Now().UTC()
	if fixture.status == "PROCESSED" {
		processedAt = now
	}
	if fixture.status == "DEAD_LETTER" {
		deadLetteredAt = now
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO outbox_events (
			id,event_name,event_version,aggregate_type,aggregate_id,partition_key,
			deduplication_key,payload,headers,status,attempt_count,max_attempts,
			available_at,lease_token,lease_generation,leased_by,lease_expires_at,
			occurred_at,processed_at,dead_lettered_at
		) VALUES ($1::uuid,'test.terminalization',1,'test',$2::uuid,$2::uuid::text,$1::uuid::text,'{}','{}',$3,$4,$5,$6,$7,1,$8,$9,$6,$10,$11)`,
		fixture.id, aggregateID, fixture.status, fixture.attempt, fixture.maxAttempts,
		now.Add(-time.Hour), leaseToken, leasedBy, fixture.leaseExpires, processedAt, deadLetteredAt); err != nil {
		t.Fatal(err)
	}
}

func claimTerminalizationBatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time, limit int, protocol string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testOutboxRepository(tx).ClaimBatch(ctx, outbox.ClaimRequest{
		Owner: "terminalization-test-worker", Limit: limit, LeaseDuration: time.Minute, Now: now,
		Protocols: []outbox.EventKey{{Name: protocol, Version: 1}},
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func readOutboxStatuses(t *testing.T, ctx context.Context, pool *pgxpool.Pool, aggregateID uuid.UUID) map[uuid.UUID]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT id,status FROM outbox_events WHERE aggregate_id=$1`, aggregateID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	statuses := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatal(err)
		}
		statuses[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return statuses
}

func TestClaimBatchBoundsExhaustedLeaseTerminalizationIntegration(t *testing.T) {
	ctx, ownerPool, workerPool := openOutboxTerminalizationPools(t)
	aggregateID := uuid.New()
	t.Cleanup(func() {
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM outbox_events WHERE aggregate_id=$1`, aggregateID)
	})

	now := time.Now().UTC()
	ids := []uuid.UUID{
		uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		uuid.MustParse("00000000-0000-0000-0000-000000000003"),
		uuid.MustParse("00000000-0000-0000-0000-000000000004"),
		uuid.MustParse("00000000-0000-0000-0000-000000000005"),
		uuid.MustParse("00000000-0000-0000-0000-000000000006"),
		uuid.MustParse("00000000-0000-0000-0000-000000000007"),
	}
	expires := []time.Time{
		now.Add(-7 * time.Minute), now.Add(-7 * time.Minute), now.Add(-6 * time.Minute),
		now.Add(-5 * time.Minute), now.Add(-5 * time.Minute), now.Add(-4 * time.Minute),
		now.Add(-3 * time.Minute),
	}
	for index, id := range ids {
		expiresAt := expires[index]
		seedOutboxState(t, ctx, ownerPool, aggregateID, outboxStateFixture{id: id, status: "PROCESSING", attempt: 3, maxAttempts: 3, leaseExpires: &expiresAt})
	}

	for cycle, expectedDead := range [][]uuid.UUID{{ids[0], ids[1], ids[2]}, {ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]}, ids} {
		claimTerminalizationBatch(t, ctx, workerPool, now, 3, "test.terminalization")
		statuses := readOutboxStatuses(t, ctx, ownerPool, aggregateID)
		expected := make(map[uuid.UUID]struct{}, len(expectedDead))
		for _, id := range expectedDead {
			expected[id] = struct{}{}
		}
		for _, id := range ids {
			want := "PROCESSING"
			if _, ok := expected[id]; ok {
				want = "DEAD_LETTER"
			}
			if statuses[id] != want {
				t.Fatalf("cycle %d event %s status=%s want=%s", cycle+1, id, statuses[id], want)
			}
		}
	}
}

func TestClaimBatchDoesNotTerminalizeUnexpiredOrRetryableLeasesIntegration(t *testing.T) {
	ctx, ownerPool, workerPool := openOutboxTerminalizationPools(t)
	aggregateID := uuid.New()
	t.Cleanup(func() {
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM outbox_events WHERE aggregate_id=$1`, aggregateID)
	})
	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	unexpired := now.Add(time.Minute)
	fixtures := []outboxStateFixture{
		{id: uuid.New(), status: "PROCESSING", attempt: 3, maxAttempts: 3, leaseExpires: &expired},
		{id: uuid.New(), status: "PROCESSING", attempt: 3, maxAttempts: 3, leaseExpires: &unexpired},
		{id: uuid.New(), status: "PROCESSING", attempt: 2, maxAttempts: 3, leaseExpires: &expired},
		{id: uuid.New(), status: "PENDING", attempt: 0, maxAttempts: 3},
		{id: uuid.New(), status: "PROCESSED", attempt: 1, maxAttempts: 3},
		{id: uuid.New(), status: "DEAD_LETTER", attempt: 3, maxAttempts: 3},
	}
	for _, fixture := range fixtures {
		seedOutboxState(t, ctx, ownerPool, aggregateID, fixture)
	}

	claimTerminalizationBatch(t, ctx, workerPool, now, 3, "test.unmatched")
	statuses := readOutboxStatuses(t, ctx, ownerPool, aggregateID)
	want := []string{"DEAD_LETTER", "PROCESSING", "PROCESSING", "PENDING", "PROCESSED", "DEAD_LETTER"}
	for index, fixture := range fixtures {
		if statuses[fixture.id] != want[index] {
			t.Fatalf("fixture %d status=%s want=%s", index, statuses[fixture.id], want[index])
		}
	}
}

func TestConcurrentWorkersBoundExhaustedLeaseTerminalizationIntegration(t *testing.T) {
	ctx, ownerPool, workerPool := openOutboxTerminalizationPools(t)
	aggregateID := uuid.New()
	t.Cleanup(func() {
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM outbox_events WHERE aggregate_id=$1`, aggregateID)
	})
	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	for index := 0; index < 6; index++ {
		seedOutboxState(t, ctx, ownerPool, aggregateID, outboxStateFixture{id: uuid.New(), status: "PROCESSING", attempt: 3, maxAttempts: 3, leaseExpires: &expired})
	}

	txOne, err := workerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = txOne.Rollback(ctx) }()
	if _, err := testOutboxRepository(txOne).ClaimBatch(ctx, outbox.ClaimRequest{Owner: "worker-one", Limit: 2, LeaseDuration: time.Minute, Now: now, Protocols: []outbox.EventKey{{Name: "test.terminalization", Version: 1}}}); err != nil {
		t.Fatal(err)
	}
	assertDeadLettersVisibleInTransaction(t, ctx, txOne, aggregateID, 2)

	txTwo, err := workerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = txTwo.Rollback(ctx) }()
	if _, err := testOutboxRepository(txTwo).ClaimBatch(ctx, outbox.ClaimRequest{Owner: "worker-two", Limit: 2, LeaseDuration: time.Minute, Now: now, Protocols: []outbox.EventKey{{Name: "test.terminalization", Version: 1}}}); err != nil {
		t.Fatal(err)
	}
	assertDeadLettersVisibleInTransaction(t, ctx, txTwo, aggregateID, 2)

	if err := txOne.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := txTwo.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	statuses := readOutboxStatuses(t, ctx, ownerPool, aggregateID)
	dead, processing := 0, 0
	for _, status := range statuses {
		switch status {
		case "DEAD_LETTER":
			dead++
		case "PROCESSING":
			processing++
		}
	}
	if dead != 4 || processing != 2 {
		t.Fatalf("dead=%d processing=%d want dead=4 processing=2", dead, processing)
	}
}

func assertDeadLettersVisibleInTransaction(t *testing.T, ctx context.Context, tx pgx.Tx, aggregateID uuid.UUID, want int) {
	t.Helper()
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id=$1 AND status='DEAD_LETTER'`, aggregateID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transaction dead-letter count=%d want=%d", count, want)
	}
}
