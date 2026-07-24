package reconciliation

import (
	"errors"

	"github.com/google/uuid"
)

type Kind string

const (
	BalanceMismatch       Kind = "BALANCE_MISMATCH"
	OrphanAllocation      Kind = "ORPHAN_ALLOCATION"
	DuplicateEffect       Kind = "DUPLICATE_EFFECT"
	MissingDocumentEffect Kind = "MISSING_DOCUMENT_EFFECT"
	MissingAudit          Kind = "MISSING_AUDIT"
	MissingOutbox         Kind = "MISSING_OUTBOX"
	InvalidMovementState  Kind = "INVALID_MOVEMENT_STATE"
	InvalidOperationState Kind = "INVALID_OPERATION_STATE"
	InvalidLotState       Kind = "INVALID_LOT_STATE"
)

var ErrInvalidScope = errors.New("invalid reconciliation scope")

type Scope struct {
	OperationIDs           []uuid.UUID
	RequiredAuditEventIDs  []uuid.UUID
	RequiredOutboxEventIDs []uuid.UUID
}

type Violation struct {
	Kind       Kind
	EntityType string
	EntityID   uuid.UUID
	Expected   int64
	Actual     int64
}

type Report struct{ Violations []Violation }

func (r Report) Clean() bool { return len(r.Violations) == 0 }
