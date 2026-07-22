package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
)

var (
	ErrMetadataRejected  = errors.New("audit metadata rejected")
	ErrDependencyMissing = errors.New("required dependency is missing")
)

type Repository interface {
	Append(context.Context, Event) error
}

type Writer struct {
	repository Repository
	policy     MetadataPolicy
}

func NewWriter(repository Repository, policy MetadataPolicy) (*Writer, error) {
	if repository == nil || policy == nil || len(policy) == 0 {
		return nil, ErrDependencyMissing
	}
	return &Writer{repository: repository, policy: clonePolicy(policy)}, nil
}

func (w *Writer) Append(ctx context.Context, event Event) error {
	if w == nil || w.repository == nil {
		return ErrDependencyMissing
	}
	if err := validateEvent(event); err != nil {
		return err
	}
	metadata, err := w.validateMetadata(event.Action, event.Metadata)
	if err != nil {
		return err
	}
	event.Metadata = metadata
	if err := w.repository.Append(ctx, event); err != nil {
		return fmt.Errorf("append mandatory audit event: %w", err)
	}
	return nil
}

func validateEvent(event Event) error {
	if event.ID == uuid.Nil || event.OccurredAt.IsZero() || strings.TrimSpace(event.Action) == "" || event.Action != strings.TrimSpace(event.Action) || len(event.Action) > 150 || strings.TrimSpace(event.ObjectType) == "" || event.ObjectType != strings.TrimSpace(event.ObjectType) || len(event.ObjectType) > 100 {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	if event.ActorType == ActorUser {
		if event.ActorUserID == nil || *event.ActorUserID == uuid.Nil {
			return &apperror.Typed{Category: apperror.ErrInvalidArgument}
		}
	} else if event.ActorType == ActorSystem {
		if event.ActorUserID != nil || event.ActorSessionID != nil {
			return &apperror.Typed{Category: apperror.ErrInvalidArgument}
		}
	} else {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	if event.ActorSessionID != nil && event.ActorUserID == nil {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	for _, id := range []*uuid.UUID{event.ActorSessionID, event.PharmacyID, event.ObjectID} {
		if id != nil && *id == uuid.Nil {
			return &apperror.Typed{Category: apperror.ErrInvalidArgument}
		}
	}
	if len(event.RequestID) > 128 || len(event.TraceID) > 128 {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	if event.Result != ResultSuccess && event.Result != ResultDenied && event.Result != ResultFailure {
		return &apperror.Typed{Category: apperror.ErrInvalidArgument}
	}
	return nil
}

func (w *Writer) validateMetadata(action string, metadata Metadata) (Metadata, error) {
	allowed, actionKnown := w.policy[action]
	if !actionKnown {
		return nil, errors.Join(ErrMetadataRejected, apperror.ErrInvalidArgument)
	}
	result := make(Metadata, len(metadata))
	for key, value := range metadata {
		expected, ok := allowed[key]
		if !ok || !metadataTypeMatches(expected, value) {
			return nil, errors.Join(ErrMetadataRejected, apperror.ErrInvalidArgument)
		}
		result[key] = value
	}
	return result, nil
}

func metadataTypeMatches(expected MetadataType, value any) bool {
	switch expected {
	case MetadataString:
		text, ok := value.(string)
		return ok && text == strings.TrimSpace(text) && text != "" && len(text) <= 512
	case MetadataBool:
		_, ok := value.(bool)
		return ok
	case MetadataInteger:
		_, ok := value.(int64)
		return ok
	case MetadataUUID:
		id, ok := value.(uuid.UUID)
		return ok && id != uuid.Nil
	default:
		return false
	}
}

func clonePolicy(policy MetadataPolicy) MetadataPolicy {
	copyPolicy := make(MetadataPolicy, len(policy))
	for action, fields := range policy {
		copyFields := make(map[string]MetadataType, len(fields))
		for field, fieldType := range fields {
			copyFields[field] = fieldType
		}
		copyPolicy[action] = copyFields
	}
	return copyPolicy
}
