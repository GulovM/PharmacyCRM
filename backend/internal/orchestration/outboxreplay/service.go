package outboxreplay

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
)

const AuditAction = "outbox.manual_replayed"

func AuditMetadataPolicy() audit.MetadataPolicy {
	return audit.MetadataPolicy{
		AuditAction: {
			"reason": audit.MetadataString,
		},
	}
}

type UnitOfWork interface {
	ReplayDeadLetter(context.Context, uuid.UUID, time.Time) error
	AppendAudit(context.Context, audit.Event) error
}

type Transactor interface {
	WithinTransaction(context.Context, func(context.Context, UnitOfWork) error) error
}

type Command struct {
	EventID        uuid.UUID
	AuditEventID   uuid.UUID
	ActorUserID    uuid.UUID
	ActorSessionID *uuid.UUID
	Reason         string
	RequestID      string
	TraceID        string
	IPAddress      netip.Addr
	UserAgent      string
	OccurredAt     time.Time
}

type Service struct{ transactor Transactor }

func NewService(transactor Transactor) *Service { return &Service{transactor: transactor} }

func (s *Service) Replay(ctx context.Context, command Command) error {
	if s == nil || s.transactor == nil || command.EventID == uuid.Nil || command.AuditEventID == uuid.Nil ||
		command.ActorUserID == uuid.Nil || command.OccurredAt.IsZero() || strings.TrimSpace(command.Reason) == "" ||
		command.Reason != strings.TrimSpace(command.Reason) || len(command.Reason) > 512 {
		return errors.Join(apperror.ErrInvalidArgument, errors.New("invalid outbox replay command"))
	}
	return s.transactor.WithinTransaction(ctx, func(ctx context.Context, unitOfWork UnitOfWork) error {
		if err := unitOfWork.ReplayDeadLetter(ctx, command.EventID, command.OccurredAt); err != nil {
			return err
		}
		auditEvent := audit.Event{
			ID: command.AuditEventID, OccurredAt: command.OccurredAt,
			ActorUserID: &command.ActorUserID, ActorSessionID: command.ActorSessionID,
			ActorType: audit.ActorUser, Action: AuditAction,
			ObjectType: "outbox_event", ObjectID: &command.EventID,
			Result: audit.ResultSuccess, RequestID: command.RequestID, TraceID: command.TraceID,
			IPAddress: command.IPAddress, UserAgent: command.UserAgent,
			Metadata: audit.Metadata{"reason": command.Reason},
		}
		return unitOfWork.AppendAudit(ctx, auditEvent)
	})
}
