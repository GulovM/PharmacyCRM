package application

import (
	"net/netip"
	"time"

	"github.com/google/uuid"
)

type ActorType string
type Result string

const (
	ActorUser     ActorType = "USER"
	ActorSystem   ActorType = "SYSTEM"
	ResultSuccess Result    = "SUCCESS"
	ResultDenied  Result    = "DENIED"
	ResultFailure Result    = "FAILURE"
)

// Metadata contains scalar, explicitly allowlisted context only. Raw request
// bodies, credentials, tokens, SQL, and stack traces are never accepted.
type Metadata map[string]any

type Event struct {
	ID             uuid.UUID
	OccurredAt     time.Time
	ActorUserID    *uuid.UUID
	ActorSessionID *uuid.UUID
	PharmacyID     *uuid.UUID
	ActorType      ActorType
	Action         string
	ObjectType     string
	ObjectID       *uuid.UUID
	Result         Result
	RequestID      string
	TraceID        string
	IPAddress      netip.Addr
	UserAgent      string
	Metadata       Metadata
}

type MetadataType uint8

const (
	MetadataString MetadataType = iota + 1
	MetadataBool
	MetadataInteger
	MetadataUUID
)

type MetadataPolicy map[string]map[string]MetadataType
