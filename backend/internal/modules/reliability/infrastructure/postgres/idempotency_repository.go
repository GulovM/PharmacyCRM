package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type IdempotencyRepository struct{ executor database.DBTX }

func NewIdempotencyRepository(executor database.DBTX) *IdempotencyRepository {
	return &IdempotencyRepository{executor: executor}
}

func (r *IdempotencyRepository) Claim(ctx context.Context, claim idempotency.Claim) (idempotency.Record, bool, error) {
	var id uuid.UUID
	err := r.executor.QueryRow(ctx, `
		INSERT INTO idempotency_records (
			actor_user_id, pharmacy_id, operation, idempotency_key, request_hash, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (scope_key, idempotency_key) DO NOTHING
		RETURNING id`, claim.Identity.ActorID, claim.Identity.PharmacyID, claim.Identity.Operation, claim.Identity.Key, claim.Fingerprint[:], claim.ExpiresAt).Scan(&id)
	if err == nil {
		return idempotency.Record{ID: idempotency.RecordID(id), Fingerprint: claim.Fingerprint, Status: idempotency.StatusInProgress}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return idempotency.Record{}, false, fmt.Errorf("insert idempotency claim: %w", err)
	}

	record, err := r.lockByIdentity(ctx, claim)
	if err != nil {
		return idempotency.Record{}, false, err
	}
	if record.Status == idempotency.StatusFailedRetryable && record.Fingerprint == claim.Fingerprint {
		tag, err := r.executor.Exec(ctx, `
			UPDATE idempotency_records
			SET status = 'IN_PROGRESS', response_status = NULL, response_body = NULL,
				resource_type = NULL, resource_id = NULL, completed_at = NULL, expires_at = $2
			WHERE id = $1 AND status = 'FAILED_RETRYABLE'`, uuid.UUID(record.ID), claim.ExpiresAt)
		if err != nil {
			return idempotency.Record{}, false, fmt.Errorf("reclaim retryable idempotency record: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return idempotency.Record{}, false, idempotency.ErrInvalidState
		}
	}
	return record, false, nil
}

func (r *IdempotencyRepository) lockByIdentity(ctx context.Context, claim idempotency.Claim) (idempotency.Record, error) {
	var record idempotency.Record
	var id uuid.UUID
	var fingerprint []byte
	var responseStatus *int
	var responseBody []byte
	var resourceType *string
	var resourceID *uuid.UUID
	err := r.executor.QueryRow(ctx, `
		SELECT id, request_hash, status, response_status, response_body, resource_type, resource_id
		FROM idempotency_records
		WHERE actor_user_id = $1 AND operation = $2
			AND (($3::uuid IS NULL AND pharmacy_id IS NULL) OR pharmacy_id = $3)
			AND idempotency_key = $4
		FOR UPDATE`, claim.Identity.ActorID, claim.Identity.Operation, claim.Identity.PharmacyID, claim.Identity.Key).
		Scan(&id, &fingerprint, &record.Status, &responseStatus, &responseBody, &resourceType, &resourceID)
	if err != nil {
		return idempotency.Record{}, fmt.Errorf("lock idempotency claim: %w", err)
	}
	if len(fingerprint) != len(record.Fingerprint) {
		return idempotency.Record{}, fmt.Errorf("invalid stored idempotency fingerprint")
	}
	copy(record.Fingerprint[:], fingerprint)
	record.ID = idempotency.RecordID(id)
	if record.Status == idempotency.StatusCompleted && responseStatus != nil {
		record.Result = &idempotency.StoredResult{ResponseStatus: *responseStatus, ResponseBody: json.RawMessage(responseBody), ResourceID: resourceID}
		if resourceType != nil {
			record.Result.ResourceType = *resourceType
		}
	}
	return record, nil
}

func (r *IdempotencyRepository) Complete(ctx context.Context, completion idempotency.Completion) error {
	tag, err := r.executor.Exec(ctx, `
		UPDATE idempotency_records
		SET status = 'COMPLETED', response_status = $2, response_body = $3::jsonb,
			resource_type = NULLIF($4, ''), resource_id = $5, completed_at = now()
		WHERE id = $1 AND status = 'IN_PROGRESS'`, uuid.UUID(completion.RecordID), completion.Result.ResponseStatus, string(completion.Result.ResponseBody), completion.Result.ResourceType, completion.Result.ResourceID)
	if err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return idempotency.ErrInvalidState
	}
	return nil
}

func (r *IdempotencyRepository) Replay(ctx context.Context, recordID idempotency.RecordID) (idempotency.StoredResult, error) {
	var result idempotency.StoredResult
	var body []byte
	var resourceType *string
	err := r.executor.QueryRow(ctx, `
		SELECT response_status, response_body, resource_type, resource_id
		FROM idempotency_records WHERE id = $1 AND status = 'COMPLETED'`, uuid.UUID(recordID)).
		Scan(&result.ResponseStatus, &body, &resourceType, &result.ResourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return idempotency.StoredResult{}, idempotency.ErrInvalidState
	}
	if err != nil {
		return idempotency.StoredResult{}, fmt.Errorf("replay idempotency result: %w", err)
	}
	result.ResponseBody = json.RawMessage(body)
	if resourceType != nil {
		result.ResourceType = *resourceType
	}
	return result, nil
}

func (r *IdempotencyRepository) MarkRetryableFailure(ctx context.Context, recordID idempotency.RecordID) error {
	tag, err := r.executor.Exec(ctx, `
		UPDATE idempotency_records SET status = 'FAILED_RETRYABLE', completed_at = now()
		WHERE id = $1 AND status = 'IN_PROGRESS'`, uuid.UUID(recordID))
	if err != nil {
		return fmt.Errorf("mark retryable idempotency failure: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return idempotency.ErrInvalidState
	}
	return nil
}
