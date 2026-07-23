package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOutboxRetentionTerminalRowsAndPrivilegesIntegration(t *testing.T) {
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	worker, err := pgxpool.New(ctx, postgrestest.WorkerRuntimeDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()

	aggregateID := uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DELETE FROM outbox_events WHERE aggregate_id=$1`, aggregateID)
	})
	oldProcessed := time.Now().Add(-31 * 24 * time.Hour)
	newProcessed := time.Now().Add(-29 * 24 * time.Hour)
	oldDead := time.Now().Add(-181 * 24 * time.Hour)
	for index, row := range []struct {
		status          string
		processed, dead *time.Time
	}{
		{"PROCESSED", &oldProcessed, nil}, {"PROCESSED", &oldProcessed, nil}, {"PROCESSED", &newProcessed, nil},
		{"DEAD_LETTER", nil, &oldDead}, {"PENDING", nil, nil}, {"PROCESSING", nil, nil},
	} {
		leaseToken, leasedBy, leaseExpires := any(nil), any(nil), any(nil)
		if row.status == "PROCESSING" {
			leaseToken, leasedBy, leaseExpires = uuid.New(), "retention-test", time.Now().Add(time.Hour)
		}
		_, err := admin.Exec(ctx, `INSERT INTO outbox_events(id,event_name,aggregate_type,aggregate_id,partition_key,deduplication_key,payload,status,occurred_at,created_at,processed_at,dead_lettered_at,lease_token,leased_by,lease_expires_at) VALUES($1,'test.retention','test',$2,$3,$4,'{}',$5,now(),now()-interval '400 days',$6,$7,$8,$9,$10)`, ids[index], aggregateID, aggregateID.String(), ids[index].String(), row.status, row.processed, row.dead, leaseToken, leasedBy, leaseExpires)
		if err != nil {
			t.Fatal(err)
		}
	}

	deleteBatch := func(method func(*TransactionalOutboxRetentionRepository) (int64, error)) int64 {
		tx, err := worker.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		repository, err := NewTransactionalOutboxRetentionRepository(database.WrapPGXTransaction(tx))
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		deleted, err := method(repository)
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return deleted
	}
	if deleted := deleteBatch(func(repository *TransactionalOutboxRetentionRepository) (int64, error) {
		return repository.DeleteProcessedBefore(ctx, time.Now().Add(-30*24*time.Hour), 1)
	}); deleted != 1 {
		t.Fatalf("first processed batch=%d", deleted)
	}
	if deleted := deleteBatch(func(repository *TransactionalOutboxRetentionRepository) (int64, error) {
		return repository.DeleteProcessedBefore(ctx, time.Now().Add(-30*24*time.Hour), 1)
	}); deleted != 1 {
		t.Fatalf("second processed batch=%d", deleted)
	}
	if deleted := deleteBatch(func(repository *TransactionalOutboxRetentionRepository) (int64, error) {
		return repository.DeleteDeadLettersBefore(ctx, time.Now().Add(-180*24*time.Hour), 10)
	}); deleted != 1 {
		t.Fatalf("dead-letter batch=%d", deleted)
	}

	var remaining int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE id=ANY($1::uuid[])`, ids).Scan(&remaining); err != nil || remaining != 3 {
		t.Fatalf("remaining=%d err=%v", remaining, err)
	}
	var statuses []string
	rows, err := admin.Query(ctx, `SELECT status FROM outbox_events WHERE id=ANY($1::uuid[]) ORDER BY status`, ids)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatal(err)
		}
		statuses = append(statuses, status)
	}
	rows.Close()
	if len(statuses) != 3 || statuses[0] != "PENDING" || statuses[1] != "PROCESSED" || statuses[2] != "PROCESSING" {
		t.Fatalf("survivors=%v", statuses)
	}

	for name, query := range map[string]string{
		"processed future cutoff":   `SELECT public.delete_processed_outbox_events_before(now() + interval '1 day', 1000)`,
		"dead-letter future cutoff": `SELECT public.delete_dead_letter_outbox_events_before(now() + interval '1 day', 1000)`,
	} {
		t.Run(name, func(t *testing.T) {
			var deleted int64
			err := worker.QueryRow(ctx, query).Scan(&deleted)
			var postgresError *pgconn.PgError
			if !errors.As(err, &postgresError) || postgresError.Code != "22023" {
				t.Fatalf("expected guarded retention cutoff, deleted=%d err=%v", deleted, err)
			}
		})
	}

	_, err = worker.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id=$1`, aggregateID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
		t.Fatalf("worker table DELETE unexpectedly allowed: %v", err)
	}
}
