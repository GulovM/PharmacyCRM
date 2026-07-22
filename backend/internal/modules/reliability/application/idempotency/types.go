package idempotency

import (
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Fingerprint [sha256.Size]byte

func NewFingerprint(canonicalPayload []byte) Fingerprint { return sha256.Sum256(canonicalPayload) }

type Identity struct {
	ActorID    uuid.UUID
	PharmacyID *uuid.UUID
	Operation  string
	Key        string
}

type Claim struct {
	Identity    Identity
	Fingerprint Fingerprint
	ExpiresAt   time.Time
}

type RecordID uuid.UUID

type ClaimState string

const (
	Claimed         ClaimState = "CLAIMED"
	ReplayAvailable ClaimState = "REPLAY_AVAILABLE"
)

type ClaimResult struct {
	RecordID RecordID
	State    ClaimState
	Replay   *StoredResult
}

type StoredResult struct {
	ResponseStatus int
	ResponseBody   json.RawMessage
	ResourceType   string
	ResourceID     *uuid.UUID
}

type Completion struct {
	RecordID RecordID
	Result   StoredResult
}
