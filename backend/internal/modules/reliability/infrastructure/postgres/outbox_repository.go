package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/outbox"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/google/uuid"
)

type TransactionalOutboxRepository struct{ executor database.TransactionExecutor }

func NewTransactionalOutboxRepository(executor database.TransactionExecutor) (*TransactionalOutboxRepository, error) {
	if executor == nil {
		return nil, database.ErrDependencyMissing
	}
	return &TransactionalOutboxRepository{executor: executor}, nil
}

func (r *TransactionalOutboxRepository) Append(ctx context.Context, event outbox.Event) error {
	if r == nil || r.executor == nil {
		return database.ErrDependencyMissing
	}
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

func (r *TransactionalOutboxRepository) ClaimBatch(ctx context.Context, request outbox.ClaimRequest) ([]outbox.Lease, error) {
	if r == nil || r.executor == nil {
		return nil, database.ErrDependencyMissing
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	eventNames := make([]string, 0, len(request.Protocols))
	eventVersions := make([]int16, 0, len(request.Protocols))
	for _, protocol := range request.Protocols {
		eventNames = append(eventNames, protocol.Name)
		eventVersions = append(eventVersions, protocol.Version)
	}
	// A worker that crashed on its final permitted attempt cannot acknowledge
	// the result. Once its lease expires, move only one bounded, deterministic
	// batch to dead letter. With the same N for terminalization and claiming, a
	// single transaction changes at most 2N rows.
	terminalizationLimit := min(request.Limit, 100)
	tag, err := r.executor.Exec(ctx, `
		WITH exhausted AS (
			SELECT id FROM outbox_events
			WHERE status = 'PROCESSING' AND lease_expires_at <= $1
			  AND attempt_count >= max_attempts
			ORDER BY lease_expires_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE outbox_events AS event
		SET status = 'DEAD_LETTER', dead_lettered_at = $1,
			last_error_code = 'LEASE_EXPIRED_AFTER_MAX_ATTEMPTS', last_error_at = $1,
			lease_token = NULL, leased_by = NULL, lease_expires_at = NULL
		FROM exhausted
		WHERE event.id = exhausted.id`, request.Now, terminalizationLimit)
	if err != nil {
		return nil, fmt.Errorf("dead-letter exhausted leases: %w", err)
	}
	if affected := tag.RowsAffected(); affected > int64(terminalizationLimit) {
		return nil, fmt.Errorf("dead-letter exhausted leases changed %d rows above limit %d", affected, terminalizationLimit)
	}

	expiresAt := request.Now.Add(request.LeaseDuration)
	rows, err := r.executor.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM outbox_events
			WHERE attempt_count < max_attempts
			  AND (event_name, event_version) IN (
				SELECT * FROM unnest($5::varchar[], $6::smallint[])
			  )
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
		request.Now, request.Limit, request.Owner, expiresAt, eventNames, eventVersions)
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

func (r *TransactionalOutboxRepository) MarkProcessed(ctx context.Context, lease outbox.Lease, completedAt time.Time) error {
	if r == nil || r.executor == nil {
		return database.ErrDependencyMissing
	}
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

func (r *TransactionalOutboxRepository) MarkFailed(ctx context.Context, lease outbox.Lease, failure outbox.Failure, failedAt, availableAt time.Time) error {
	if r == nil || r.executor == nil {
		return database.ErrDependencyMissing
	}
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

func (r *TransactionalOutboxRepository) ReplayDeadLetter(ctx context.Context, id uuid.UUID, availableAt time.Time) error {
	if r == nil || r.executor == nil {
		return database.ErrDependencyMissing
	}
	var replayed bool
	if err := r.executor.QueryRow(ctx, "SELECT public.replay_dead_letter_outbox_event($1, $2)", id, availableAt).Scan(&replayed); err != nil {
		return fmt.Errorf("replay dead-letter outbox event: %w", err)
	}
	if !replayed {
		return outbox.ErrNotDeadLetter
	}
	return nil
}

type OutboxTransactor struct {
	runner *database.TransactionRunner[outbox.Repository]
}

func NewOutboxTransactor(pool *database.Pool, observer database.RollbackErrorObserver) (*OutboxTransactor, error) {
	runner, err := database.NewTransactionRunner(
		pool,
		func(executor database.TransactionExecutor) (outbox.Repository, error) {
			return NewTransactionalOutboxRepository(executor)
		},
		observer,
	)
	if err != nil {
		return nil, err
	}
	return &OutboxTransactor{runner: runner}, nil
}

func (t *OutboxTransactor) WithinTransaction(ctx context.Context, fn func(context.Context, outbox.Repository) error) error {
	if t == nil || t.runner == nil {
		return database.ErrDependencyMissing
	}
	return t.runner.WithinTransaction(ctx, fn)
}

var _ outbox.Repository = (*TransactionalOutboxRepository)(nil)
var _ outbox.Transactor = (*OutboxTransactor)(nil)
