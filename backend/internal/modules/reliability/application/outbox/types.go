package outbox

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

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
	Owner         string
	Limit         int
	LeaseDuration time.Duration
	Now           time.Time
}

type Failure struct {
	Code      string
	Retryable bool
}

type Handler interface {
	Handle(context.Context, Event) error
}
