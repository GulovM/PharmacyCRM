package outboxreplay

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"time"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
)

const (
	AuditAction = "outbox.manual_replayed"
	Operation   = "outbox.manual_replay"
)

var ErrInvalidReplayState = errors.New("invalid manual outbox replay state")

func AuditMetadataPolicy() audit.MetadataPolicy {
	return audit.MetadataPolicy{AuditAction: {"reason": audit.MetadataString}}
}

type Actor struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

type UnitOfWork interface {
	ClaimIdempotency(context.Context, idempotency.Claim) (idempotency.ClaimResult, error)
	RevalidateAdmin(context.Context, Actor) error
	ReplayDeadLetter(context.Context, uuid.UUID, time.Time) error
	AppendAudit(context.Context, audit.Event) error
	CompleteIdempotency(context.Context, idempotency.Completion) error
}

type Transactor interface {
	WithinTransaction(context.Context, func(context.Context, UnitOfWork) error) error
}

type Command struct {
	EventID              uuid.UUID
	AuditEventID         uuid.UUID
	ActorUserID          uuid.UUID
	ActorSessionID       uuid.UUID
	Reason               string
	IdempotencyKey       string
	Fingerprint          idempotency.Fingerprint
	IdempotencyExpiresAt time.Time
	RequestID            string
	TraceID              string
	IPAddress            netip.Addr
	UserAgent            string
	OccurredAt           time.Time
}

type Result struct{ IdempotencyReplayed bool }

type Service struct{ transactor Transactor }

func NewService(transactor Transactor) *Service { return &Service{transactor: transactor} }

func (s *Service) Replay(ctx context.Context, command Command) (Result, error) {
	if err := validateCommand(s, command); err != nil {
		return Result{}, err
	}
	var result Result
	err := s.transactor.WithinTransaction(ctx, func(ctx context.Context, unitOfWork UnitOfWork) error {
		claim, err := unitOfWork.ClaimIdempotency(ctx, idempotency.Claim{
			Identity:    idempotency.Identity{ActorID: command.ActorUserID, Operation: Operation, Key: command.IdempotencyKey},
			Fingerprint: command.Fingerprint,
			ExpiresAt:   command.IdempotencyExpiresAt,
		})
		if err != nil {
			return err
		}
		if err := unitOfWork.RevalidateAdmin(ctx, Actor{UserID: command.ActorUserID, SessionID: command.ActorSessionID}); err != nil {
			return err
		}
		if claim.State == idempotency.ReplayAvailable {
			if !validReplay(claim.Replay, command.EventID) {
				return errors.Join(ErrInvalidReplayState, apperror.ErrConflict)
			}
			result.IdempotencyReplayed = true
			return nil
		}
		if claim.State != idempotency.Claimed {
			return errors.Join(ErrInvalidReplayState, apperror.ErrConflict)
		}
		if err := unitOfWork.ReplayDeadLetter(ctx, command.EventID, command.OccurredAt); err != nil {
			return err
		}
		auditEvent := audit.Event{
			ID: command.AuditEventID, OccurredAt: command.OccurredAt,
			ActorUserID: &command.ActorUserID, ActorSessionID: &command.ActorSessionID,
			ActorType: audit.ActorUser, Action: AuditAction,
			ObjectType: "outbox_event", ObjectID: &command.EventID,
			Result: audit.ResultSuccess, RequestID: command.RequestID, TraceID: command.TraceID,
			IPAddress: command.IPAddress, UserAgent: command.UserAgent,
			Metadata: audit.Metadata{"reason": command.Reason},
		}
		if err := unitOfWork.AppendAudit(ctx, auditEvent); err != nil {
			return err
		}
		resourceID := command.EventID
		return unitOfWork.CompleteIdempotency(ctx, idempotency.Completion{
			RecordID: claim.RecordID,
			Result: idempotency.StoredResult{
				ResponseStatus: 200,
				ResponseBody:   json.RawMessage(`{"replayed":true}`),
				ResourceType:   "outbox_event",
				ResourceID:     &resourceID,
			},
		})
	})
	return result, err
}

func validateCommand(service *Service, command Command) error {
	var zeroFingerprint idempotency.Fingerprint
	if service == nil || service.transactor == nil || command.EventID == uuid.Nil || command.AuditEventID == uuid.Nil ||
		command.ActorUserID == uuid.Nil || command.ActorSessionID == uuid.Nil || command.OccurredAt.IsZero() ||
		command.IdempotencyExpiresAt.IsZero() || command.Fingerprint == zeroFingerprint ||
		strings.TrimSpace(command.IdempotencyKey) == "" || command.IdempotencyKey != strings.TrimSpace(command.IdempotencyKey) || len(command.IdempotencyKey) > 128 ||
		strings.TrimSpace(command.Reason) == "" || command.Reason != strings.TrimSpace(command.Reason) || len(command.Reason) > 512 {
		return errors.Join(apperror.ErrInvalidArgument, errors.New("invalid outbox replay command"))
	}
	return nil
}

func validReplay(replay *idempotency.StoredResult, eventID uuid.UUID) bool {
	return replay != nil && replay.ResponseStatus == 200 && replay.ResourceType == "outbox_event" &&
		replay.ResourceID != nil && *replay.ResourceID == eventID && json.Valid(replay.ResponseBody)
}
