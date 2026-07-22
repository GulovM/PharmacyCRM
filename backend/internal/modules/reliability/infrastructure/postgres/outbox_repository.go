package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/google/uuid"
)

type OutboxRepository struct{ executor database.DBTX }

func NewOutboxRepository(executor database.DBTX) *OutboxRepository {
	return &OutboxRepository{executor: executor}
}

func (r *OutboxRepository) Append(ctx context.Context, event outbox.Event) error {
	if event.Headers == nil {
		event.Headers = map[string]string{}
	}
	headers, err := json.Marshal(event.Headers)
	if err != nil {
		return fmt.Errorf("encode outbox headers: %w", err)
	}
	_, err = r.executor.Exec(ctx, `
		INSERT INTO outbox_events (
			id, event_name, event_version, aggregate_type, aggregate_id,
			partition_key, deduplication_key, payload, headers,
			max_attempts, occurred_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,$11)`,
		event.ID, event.EventKey.Name, event.EventKey.Version, event.AggregateType, event.AggregateID,
		event.PartitionKey, event.DeduplicationKey, string(event.Payload), string(headers),
		event.MaxAttempts, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func (r *OutboxRepository) ClaimBatch(ctx context.Context, request outbox.ClaimRequest) ([]outbox.Lease, error) {
	// A worker that crashed on its final permitted attempt cannot acknowledge
	// the result. Once its lease expires, move it to the dead-letter state so
	// the row cannot remain PROCESSING forever.
	if _, err := r.executor.Exec(ctx, `
		WITH exhausted AS (
			SELECT id FROM outbox_events
			WHERE status = 'PROCESSING' AND lease_expires_at <= $1
			  AND attempt_count >= max_attempts
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_events AS event
		SET status = 'DEAD_LETTER', dead_lettered_at = $1,
			last_error_code = 'LEASE_EXPIRED_AFTER_MAX_ATTEMPTS', last_error_at = $1,
			lease_token = NULL, leased_by = NULL, lease_expires_at = NULL
		FROM exhausted
		WHERE event.id = exhausted.id`, request.Now); err != nil {
		return nil, fmt.Errorf("dead-letter exhausted leases: %w", err)
	}

	expiresAt := request.Now.Add(request.LeaseDuration)
	rows, err := r.executor.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM outbox_events
			WHERE attempt_count < max_attempts
			  AND ((status = 'PENDING' AND available_at <= $1)
			       OR (status = 'PROCESSING' AND lease_expires_at <= $1))
			ORDER BY available_at, created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE outbox_events AS event
		SET status = 'PROCESSING', attempt_count = event.attempt_count + 1,
			lease_token = gen_random_uuid(), lease_generation = event.lease_generation + 1,
			leased_by = $3, lease_expires_at = $4
		FROM candidates
		WHERE event.id = candidates.id
		RETURNING event.id, event.event_name, event.event_version,
			event.aggregate_type, event.aggregate_id, event.partition_key,
			event.deduplication_key, event.payload, event.headers,
			event.occurred_at, event.max_attempts, event.lease_token,
			event.lease_generation, event.leased_by, event.attempt_count, event.lease_expires_at`,
		request.Now, request.Limit, request.Owner, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("claim outbox batch: %w", err)
	}
	defer rows.Close()

	leases := make([]outbox.Lease, 0, request.Limit)
	for rows.Next() {
		var lease outbox.Lease
		if err := rows.Scan(
			&lease.ID, &lease.EventKey.Name, &lease.EventKey.Version,
			&lease.AggregateType, &lease.AggregateID, &lease.PartitionKey,
			&lease.DeduplicationKey, &lease.Payload, &lease.Headers,
			&lease.OccurredAt, &lease.MaxAttempts, &lease.Token,
			&lease.Generation, &lease.Owner, &lease.Attempt, &lease.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan outbox lease: %w", err)
		}
		leases = append(leases, lease)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox leases: %w", err)
	}
	return leases, nil
}

func (r *OutboxRepository) MarkProcessed(ctx context.Context, lease outbox.Lease, completedAt time.Time) error {
	tag, err := r.executor.Exec(ctx, `
		UPDATE outbox_events
		SET status = 'PROCESSED', processed_at = $5,
			lease_token = NULL, leased_by = NULL, lease_expires_at = NULL
		WHERE id = $1 AND status = 'PROCESSING'
		  AND lease_token = $2 AND lease_generation = $3
		  AND leased_by = $4 AND lease_expires_at > $5`, lease.ID, lease.Token, lease.Generation, lease.Owner, completedAt)
	if err != nil {
		return fmt.Errorf("mark outbox event processed: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return outbox.ErrStaleLease
	}
	return nil
}

func (r *OutboxRepository) MarkFailed(ctx context.Context, lease outbox.Lease, failure outbox.Failure, failedAt, availableAt time.Time) error {
	terminal := !failure.Retryable || lease.Attempt >= lease.MaxAttempts
	tag, err := r.executor.Exec(ctx, `
		UPDATE outbox_events
		SET status = CASE WHEN $6 THEN 'DEAD_LETTER' ELSE 'PENDING' END,
			available_at = CASE WHEN $6 THEN available_at ELSE $7 END,
			dead_lettered_at = CASE WHEN $6 THEN $5 ELSE NULL END,
			last_error_code = $8, last_error_at = $5,
			lease_token = NULL, leased_by = NULL, lease_expires_at = NULL
		WHERE id = $1 AND status = 'PROCESSING'
		  AND lease_token = $2 AND lease_generation = $3
		  AND leased_by = $4 AND lease_expires_at > $5`,
		lease.ID, lease.Token, lease.Generation, lease.Owner, failedAt, terminal, availableAt, failure.Code)
	if err != nil {
		return fmt.Errorf("mark outbox event failed: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return outbox.ErrStaleLease
	}
	return nil
}

func (r *OutboxRepository) ReplayDeadLetter(ctx context.Context, id uuid.UUID, availableAt time.Time) error {
	tag, err := r.executor.Exec(ctx, `
		UPDATE outbox_events
		SET status = 'PENDING', attempt_count = 0, available_at = $2,
			last_error_code = NULL, last_error_at = NULL, dead_lettered_at = NULL,
			lease_token = NULL, leased_by = NULL, lease_expires_at = NULL
		WHERE id = $1 AND status = 'DEAD_LETTER'`, id, availableAt)
	if err != nil {
		return fmt.Errorf("replay dead-letter event: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return outbox.ErrNotDeadLetter
	}
	return nil
}

type OutboxTransactor struct {
	runner *database.TransactionRunner[outbox.Repository]
}

func NewOutboxTransactor(pool *database.Pool, observer database.RollbackErrorObserver) *OutboxTransactor {
	return &OutboxTransactor{runner: database.NewTransactionRunner(
		pool,
		func(executor database.DBTX) outbox.Repository { return NewOutboxRepository(executor) },
		observer,
	)}
}

func (t *OutboxTransactor) WithinTransaction(ctx context.Context, fn func(context.Context, outbox.Repository) error) error {
	if t == nil || t.runner == nil {
		return errors.New("outbox transactor is not configured")
	}
	return t.runner.WithinTransaction(ctx, fn)
}

var _ outbox.Repository = (*OutboxRepository)(nil)
var _ outbox.Transactor = (*OutboxTransactor)(nil)
