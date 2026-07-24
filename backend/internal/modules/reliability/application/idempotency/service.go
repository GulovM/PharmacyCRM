package idempotency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
)

type RecordStatus string

const (
	StatusInProgress      RecordStatus = "IN_PROGRESS"
	StatusCompleted       RecordStatus = "COMPLETED"
	StatusFailedRetryable RecordStatus = "FAILED_RETRYABLE"
)

type Record struct {
	ID          RecordID
	Fingerprint Fingerprint
	Status      RecordStatus
	Result      *StoredResult
}

type Repository interface {
	Claim(context.Context, Claim) (record Record, inserted bool, err error)
	Complete(context.Context, Completion) error
	Replay(context.Context, RecordID) (StoredResult, error)
	MarkRetryableFailure(context.Context, RecordID) error
}

const defaultClaimWait = 2 * time.Second

type Service struct {
	repository Repository
	claimWait  time.Duration
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, ErrDependencyMissing
	}
	return &Service{repository: repository, claimWait: defaultClaimWait}, nil
}

func (s *Service) Claim(ctx context.Context, claim Claim) (ClaimResult, error) {
	if err := validateClaim(claim); err != nil {
		return ClaimResult{}, err
	}
	claimCtx, cancel := context.WithTimeout(ctx, s.claimWait)
	defer cancel()
	record, inserted, err := s.repository.Claim(claimCtx, claim)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return ClaimResult{}, concurrentModificationError()
		}
		return ClaimResult{}, err
	}
	if !bytes.Equal(record.Fingerprint[:], claim.Fingerprint[:]) {
		return ClaimResult{}, keyReusedError()
	}
	if inserted || record.Status == StatusFailedRetryable {
		return ClaimResult{RecordID: record.ID, State: Claimed}, nil
	}
	if record.Status == StatusCompleted {
		result, err := s.Replay(ctx, record.ID)
		if err != nil {
			return ClaimResult{}, err
		}
		return ClaimResult{RecordID: record.ID, State: ReplayAvailable, Replay: &result}, nil
	}
	return ClaimResult{}, concurrentModificationError()
}

func (s *Service) Complete(ctx context.Context, completion Completion) error {
	if uuid.UUID(completion.RecordID) == uuid.Nil || completion.Result.ResponseStatus < 100 || completion.Result.ResponseStatus > 599 || !validJSON(completion.Result.ResponseBody) {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	return s.repository.Complete(ctx, completion)
}

func (s *Service) Replay(ctx context.Context, id RecordID) (StoredResult, error) {
	if uuid.UUID(id) == uuid.Nil {
		return StoredResult{}, &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	return s.repository.Replay(ctx, id)
}

func (s *Service) MarkRetryableFailure(ctx context.Context, id RecordID, committedEffectAbsent bool) error {
	if uuid.UUID(id) == uuid.Nil {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	if !committedEffectAbsent {
		return errors.Join(ErrInvalidState, apperror.ErrConflict)
	}
	return s.repository.MarkRetryableFailure(ctx, id)
}

func validateClaim(claim Claim) error {
	if strings.TrimSpace(claim.Identity.Key) == "" {
		return keyRequiredError()
	}
	if claim.Identity.Key != strings.TrimSpace(claim.Identity.Key) || len(claim.Identity.Key) > 128 || claim.Identity.ActorID == uuid.Nil || claim.ExpiresAt.IsZero() || !claim.ExpiresAt.After(time.Now()) {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	operation := strings.TrimSpace(claim.Identity.Operation)
	if operation == "" || operation != claim.Identity.Operation || len(operation) > 150 {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	if claim.Identity.PharmacyID != nil && *claim.Identity.PharmacyID == uuid.Nil {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	return nil
}

func validJSON(value json.RawMessage) bool {
	return len(value) > 0 && json.Valid(value) && string(value) != "null"
}
