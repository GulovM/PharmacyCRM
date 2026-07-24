package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
)

type fakeRepository struct {
	record   Record
	inserted bool
	result   StoredResult
	marked   bool
}

type blockingRepository struct{ fakeRepository }

func (b *blockingRepository) Claim(ctx context.Context, _ Claim) (Record, bool, error) {
	<-ctx.Done()
	return Record{}, false, ctx.Err()
}

func (f *fakeRepository) Claim(context.Context, Claim) (Record, bool, error) {
	return f.record, f.inserted, nil
}
func (f *fakeRepository) Complete(context.Context, Completion) error { return nil }
func (f *fakeRepository) Replay(context.Context, RecordID) (StoredResult, error) {
	return f.result, nil
}
func (f *fakeRepository) MarkRetryableFailure(context.Context, RecordID) error {
	f.marked = true
	return nil
}

func validClaim() Claim {
	return Claim{Identity: Identity{ActorID: uuid.New(), Operation: "sale.complete", Key: "key-1"}, Fingerprint: NewFingerprint([]byte(`{"quantity":1}`)), ExpiresAt: time.Now().Add(time.Hour)}
}

func mustNewService(t *testing.T, repository Repository) *Service {
	t.Helper()
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestServiceClaimsNewIdentity(t *testing.T) {
	claim := validClaim()
	id := RecordID(uuid.New())
	repository := &fakeRepository{record: Record{ID: id, Fingerprint: claim.Fingerprint, Status: StatusInProgress}, inserted: true}
	result, err := mustNewService(t, repository).Claim(context.Background(), claim)
	if err != nil || result.State != Claimed || result.RecordID != id {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestServiceReplaysCompletedIdentity(t *testing.T) {
	claim := validClaim()
	id := RecordID(uuid.New())
	stored := StoredResult{ResponseStatus: 201, ResponseBody: json.RawMessage(`{"id":"one"}`)}
	repository := &fakeRepository{record: Record{ID: id, Fingerprint: claim.Fingerprint, Status: StatusCompleted}, result: stored}
	result, err := mustNewService(t, repository).Claim(context.Background(), claim)
	if err != nil || result.State != ReplayAvailable || result.Replay == nil || string(result.Replay.ResponseBody) != string(stored.ResponseBody) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestServiceRejectsFingerprintConflictAndInProgress(t *testing.T) {
	claim := validClaim()
	repository := &fakeRepository{record: Record{ID: RecordID(uuid.New()), Fingerprint: NewFingerprint([]byte("different")), Status: StatusCompleted}}
	if _, err := mustNewService(t, repository).Claim(context.Background(), claim); !errors.Is(err, ErrKeyReused) || !errors.Is(err, apperror.ErrConflict) {
		t.Fatalf("unexpected error: %v", err)
	}
	repository.record.Fingerprint = claim.Fingerprint
	repository.record.Status = StatusInProgress
	if _, err := mustNewService(t, repository).Claim(context.Background(), claim); !errors.Is(err, ErrConcurrentModification) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceValidatesKeyAndRetryableFailureEvidence(t *testing.T) {
	claim := validClaim()
	claim.Identity.Key = ""
	service := mustNewService(t, &fakeRepository{})
	if _, err := service.Claim(context.Background(), claim); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("unexpected error: %v", err)
	}
	id := RecordID(uuid.New())
	if err := service.MarkRetryableFailure(context.Background(), id, false); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("unexpected error: %v", err)
	}
	repository := &fakeRepository{}
	if err := mustNewService(t, repository).MarkRetryableFailure(context.Background(), id, true); err != nil || !repository.marked {
		t.Fatalf("marked=%v err=%v", repository.marked, err)
	}
}

func TestServiceBoundsConcurrentClaimWait(t *testing.T) {
	service := mustNewService(t, &blockingRepository{})
	service.claimWait = time.Millisecond
	if _, err := service.Claim(context.Background(), validClaim()); !errors.Is(err, ErrConcurrentModification) || !errors.Is(err, apperror.ErrConflict) {
		t.Fatalf("unexpected error: %v", err)
	}
}
