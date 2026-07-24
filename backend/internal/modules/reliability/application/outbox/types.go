package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/contracts"
	"github.com/google/uuid"
)

var ErrInvalidClaimRequest = errors.New("invalid outbox claim request")

type EventKey struct {
	Name    string
	Version int16
}

type Event struct {
	ID               uuid.UUID
	EventKey         EventKey
	AggregateType    string
	AggregateID      uuid.UUID
	PartitionKey     string
	DeduplicationKey string
	Payload          json.RawMessage
	Headers          map[string]string
	OccurredAt       time.Time
	MaxAttempts      int16
}

type Lease struct {
	Event
	Token      uuid.UUID
	Generation int64
	Owner      string
	Attempt    int16
	ExpiresAt  time.Time
}

type ClaimRequest struct {
	Owner           string
	Limit           int
	LeaseDuration   time.Duration
	Protocols       []EventKey
	MaintenanceOnly bool
}

func (request ClaimRequest) Validate() error {
	owner := strings.TrimSpace(request.Owner)
	if owner == "" || owner != request.Owner || len(owner) > contracts.MaxWorkerOwnerLength || request.Limit < 1 || request.Limit > 100 || request.LeaseDuration <= 0 || request.LeaseDuration.Milliseconds() < 1 {
		return errors.Join(ErrInvalidClaimRequest, apperror.ErrInvalidArgument)
	}
	if request.MaintenanceOnly {
		if len(request.Protocols) != 0 {
			return errors.Join(ErrInvalidClaimRequest, apperror.ErrInvalidArgument)
		}
		return nil
	}
	if len(request.Protocols) == 0 {
		return errors.Join(ErrInvalidClaimRequest, apperror.ErrInvalidArgument)
	}
	seen := make(map[EventKey]struct{}, len(request.Protocols))
	for _, protocol := range request.Protocols {
		name := strings.TrimSpace(protocol.Name)
		if name == "" || name != protocol.Name || len(name) > 150 || protocol.Version < 1 {
			return errors.Join(ErrInvalidClaimRequest, apperror.ErrInvalidArgument)
		}
		if _, duplicate := seen[protocol]; duplicate {
			return errors.Join(ErrInvalidClaimRequest, apperror.ErrInvalidArgument)
		}
		seen[protocol] = struct{}{}
	}
	return nil
}

type Failure struct {
	Code      string
	Retryable bool
}

type Handler interface {
	Handle(context.Context, Event) error
}
