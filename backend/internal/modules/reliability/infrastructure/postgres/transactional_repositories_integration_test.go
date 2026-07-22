package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	auditpostgres "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/infrastructure/postgres"
	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type atomicReliabilityUnitOfWork interface {
	ChangeDisplayName(context.Context, uuid.UUID, string) error
	AppendAudit(context.Context, audit.Event) error
	AppendOutbox(context.Context, outbox.Event) error
	CompleteIdempotency(context.Context, idempotency.Completion) error
}

type atomicReliabilityAdapters struct {
	executor    database.TransactionExecutor
	audit       *audit.Writer
	outbox      *outbox.Writer
	idempotency *idempotency.Service
}

func newAtomicReliabilityAdapters(executor database.TransactionExecutor) (atomicReliabilityUnitOfWork, error) {
	auditRepository, err := auditpostgres.NewTransactionalAuditRepository(executor)
	if err != nil {
		return nil, err
	}
	auditWriter, err := audit.NewWriter(
		auditRepository,
		audit.MetadataPolicy{"test.atomic": {"reason": audit.MetadataString}},
	)
	if err != nil {
		return nil, err
	}
	outboxRepository, err := NewTransactionalOutboxRepository(executor)
	if err != nil {
		return nil, err
	}
	outboxWriter, err := outbox.NewWriter(
		outboxRepository,
		map[outbox.EventKey]outbox.PayloadValidator{
			{Name: "test.atomic", Version: 1}: outbox.PayloadValidatorFunc(func(json.RawMessage) error { return nil }),
		},
	)
	if err != nil {
		return nil, err
	}
	idempotencyRepository, err := NewTransactionalIdempotencyRepository(executor)
	if err != nil {
		return nil, err
	}
	idempotencyService, err := idempotency.NewService(idempotencyRepository)
	if err != nil {
		return nil, err
	}
	return &atomicReliabilityAdapters{
		executor:    executor,
		audit:       auditWriter,
		outbox:      outboxWriter,
		idempotency: idempotencyService,
	}, nil
}

func (u *atomicReliabilityAdapters) ChangeDisplayName(ctx context.Context, id uuid.UUID, name string) error {
	_, err := u.executor.Exec(ctx, "UPDATE users SET display_name = $2 WHERE id = $1", id, name)
	return err
}

func (u *atomicReliabilityAdapters) AppendAudit(ctx context.Context, event audit.Event) error {
	return u.audit.Append(ctx, event)
}

func (u *atomicReliabilityAdapters) AppendOutbox(ctx context.Context, event outbox.Event) error {
	return u.outbox.Append(ctx, event)
}

func (u *atomicReliabilityAdapters) CompleteIdempotency(ctx context.Context, completion idempotency.Completion) error {
	return u.idempotency.Complete(ctx, completion)
}

func TestMandatoryReliabilityFailuresRollbackBusinessWriteIntegration(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	rawPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rawPool.Close)
	actorID, aggregateID, existingOutboxID := uuid.New(), uuid.New(), uuid.New()
	deduplicationKey := "atomic-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM outbox_events WHERE aggregate_id = $1", aggregateID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", actorID)
	})
	if _, err := rawPool.Exec(ctx, "INSERT INTO users (id,login,password_hash,display_name) VALUES ($1,$2,'hash','Before')", actorID, "atomic-"+actorID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := rawPool.Exec(ctx, `INSERT INTO outbox_events (
		id,event_name,event_version,aggregate_type,aggregate_id,partition_key,
		deduplication_key,payload,occurred_at
	) VALUES ($1,'test.atomic',1,'test',$2::uuid,($2::uuid)::text,$3,'{}',now())`, existingOutboxID, aggregateID, deduplicationKey); err != nil {
		t.Fatal(err)
	}

	pool, err := database.NewWorker(ctx, config.WorkerPostgresConfig{
		DSN: dsn,
		PoolConfig: config.PoolConfig{
			MinConnections: 1, MaxConnections: 2, AcquireTimeout: time.Second,
			MaxConnectionLife: time.Minute, MaxConnectionIdle: time.Minute, HealthCheckPeriod: time.Minute,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	runner, err := database.NewTransactionRunner(pool, newAtomicReliabilityAdapters, nil)
	if err != nil {
		t.Fatal(err)
	}

	assertRolledBack := func(name string, callback func(context.Context, atomicReliabilityUnitOfWork) error) {
		t.Helper()
		err := runner.WithinTransaction(ctx, func(ctx context.Context, unitOfWork atomicReliabilityUnitOfWork) error {
			if err := unitOfWork.ChangeDisplayName(ctx, actorID, name); err != nil {
				return err
			}
			return callback(ctx, unitOfWork)
		})
		if err == nil {
			t.Fatalf("%s: expected reliability failure", name)
		}
		var displayName string
		if queryErr := rawPool.QueryRow(ctx, "SELECT display_name FROM users WHERE id = $1", actorID).Scan(&displayName); queryErr != nil {
			t.Fatal(queryErr)
		}
		if displayName != "Before" {
			t.Fatalf("%s: business write survived as %q", name, displayName)
		}
	}

	missingSessionID := uuid.New()
	assertRolledBack("Audit Failure", func(ctx context.Context, unitOfWork atomicReliabilityUnitOfWork) error {
		return unitOfWork.AppendAudit(ctx, audit.Event{
			ID: uuid.New(), OccurredAt: time.Now(), ActorUserID: &actorID, ActorSessionID: &missingSessionID,
			ActorType: audit.ActorUser, Action: "test.atomic", ObjectType: "user", ObjectID: &actorID,
			Result: audit.ResultSuccess, Metadata: audit.Metadata{"reason": "force failure"},
		})
	})
	assertRolledBack("Outbox Failure", func(ctx context.Context, unitOfWork atomicReliabilityUnitOfWork) error {
		return unitOfWork.AppendOutbox(ctx, outbox.Event{
			ID: uuid.New(), EventKey: outbox.EventKey{Name: "test.atomic", Version: 1},
			AggregateType: "test", AggregateID: aggregateID, PartitionKey: aggregateID.String(),
			DeduplicationKey: deduplicationKey, Payload: []byte(`{}`), OccurredAt: time.Now(),
		})
	})
	assertRolledBack("Idempotency Failure", func(ctx context.Context, unitOfWork atomicReliabilityUnitOfWork) error {
		err := unitOfWork.CompleteIdempotency(ctx, idempotency.Completion{
			RecordID: idempotency.RecordID(uuid.New()),
			Result:   idempotency.StoredResult{ResponseStatus: 200, ResponseBody: []byte(`{"ok":true}`)},
		})
		if !errors.Is(err, idempotency.ErrInvalidState) {
			t.Fatalf("unexpected idempotency error: %v", err)
		}
		return err
	})
}
