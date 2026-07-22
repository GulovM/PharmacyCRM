package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
)

const (
	DefaultMaxAttempts = int16(8)
	maxPayloadBytes    = 262144
)

var (
	ErrInvalidEvent      = errors.New("invalid outbox event")
	ErrStaleLease        = errors.New("stale outbox lease")
	ErrNotDeadLetter     = errors.New("outbox event is not dead lettered")
	ErrDependencyMissing = errors.New("required dependency is missing")
)

type Repository interface {
	Append(context.Context, Event) error
	ClaimBatch(context.Context, ClaimRequest) ([]Lease, error)
	MarkProcessed(context.Context, Lease, time.Time) error
	MarkFailed(context.Context, Lease, Failure, time.Time, time.Time) error
	ReplayDeadLetter(context.Context, uuid.UUID, time.Time) error
}

type PayloadValidator interface {
	ValidatePayload(json.RawMessage) error
}

type PayloadValidatorFunc func(json.RawMessage) error

func (f PayloadValidatorFunc) ValidatePayload(payload json.RawMessage) error { return f(payload) }

type Writer struct {
	repository Repository
	validators map[EventKey]PayloadValidator
}

func NewWriter(repository Repository, validators map[EventKey]PayloadValidator) (*Writer, error) {
	if repository == nil {
		return nil, ErrDependencyMissing
	}
	registered := make(map[EventKey]PayloadValidator, len(validators))
	for key, validator := range validators {
		if validator == nil {
			return nil, ErrDependencyMissing
		}
		registered[key] = validator
	}
	return &Writer{repository: repository, validators: registered}, nil
}

func (w *Writer) Append(ctx context.Context, event Event) error {
	if w == nil || w.repository == nil {
		return ErrDependencyMissing
	}
	if event.MaxAttempts == 0 {
		event.MaxAttempts = DefaultMaxAttempts
	}
	if err := validateEvent(event); err != nil {
		return err
	}
	validator, registered := w.validators[event.EventKey]
	if !registered || validator == nil {
		return errors.Join(ErrInvalidEvent, errors.New("unregistered outbox payload protocol"))
	}
	if err := validator.ValidatePayload(event.Payload); err != nil {
		return errors.Join(ErrInvalidEvent, apperror.ErrInvalidArgument)
	}
	return w.repository.Append(ctx, event)
}

func validateEvent(event Event) error {
	if event.ID == uuid.Nil || event.AggregateID == uuid.Nil || event.OccurredAt.IsZero() || event.EventKey.Version < 1 || event.MaxAttempts < 1 || event.MaxAttempts > 20 {
		return errors.Join(ErrInvalidEvent, apperror.ErrInvalidArgument)
	}
	for value, maximum := range map[string]int{event.EventKey.Name: 150, event.AggregateType: 100, event.PartitionKey: 200, event.DeduplicationKey: 255} {
		if value == "" || value != strings.TrimSpace(value) || len(value) > maximum {
			return errors.Join(ErrInvalidEvent, apperror.ErrInvalidArgument)
		}
	}
	if len(event.Payload) == 0 || len(event.Payload) > maxPayloadBytes || !json.Valid(event.Payload) || !safeJSONObject(event.Payload) {
		return errors.Join(ErrInvalidEvent, apperror.ErrInvalidArgument)
	}
	for key, value := range event.Headers {
		if !safeName(key) || len(value) > 512 || forbiddenName(key) {
			return errors.Join(ErrInvalidEvent, apperror.ErrInvalidArgument)
		}
	}
	return nil
}

func safeJSONObject(payload json.RawMessage) bool {
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return false
	}
	object, ok := value.(map[string]any)
	return ok && safeJSONValue(object)
}

func safeJSONValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if !safeName(key) || forbiddenName(key) || !safeJSONValue(nested) {
				return false
			}
		}
		return true
	case []any:
		if len(typed) > 1000 {
			return false
		}
		for _, nested := range typed {
			if !safeJSONValue(nested) {
				return false
			}
		}
		return true
	case string:
		return len(typed) <= 4096
	case float64, bool, nil:
		return true
	default:
		return false
	}
}

func safeName(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 150
}

func forbiddenName(value string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(value, "-", "_"))
	for _, fragment := range []string{"password", "secret", "token", "authorization", "cookie", "private_key", "dsn", "credential"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
