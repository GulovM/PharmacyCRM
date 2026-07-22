package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepository struct {
	event Event
	err   error
}

func (f *fakeRepository) Append(_ context.Context, event Event) error { f.event = event; return f.err }

func testEvent() Event {
	actorID := uuid.New()
	return Event{ID: uuid.New(), OccurredAt: time.Now(), ActorUserID: &actorID, ActorType: ActorUser, Action: "test.changed", ObjectType: "test", Result: ResultSuccess, Metadata: Metadata{"reason": "approved", "count": int64(1)}}
}

func TestWriterAcceptsOnlyActionAllowlistedScalarMetadata(t *testing.T) {
	repository := &fakeRepository{}
	writer := NewWriter(repository, MetadataPolicy{"test.changed": {"reason": MetadataString, "count": MetadataInteger}})
	if err := writer.Append(context.Background(), testEvent()); err != nil {
		t.Fatal(err)
	}
	if repository.event.Metadata["reason"] != "approved" {
		t.Fatalf("unexpected metadata: %#v", repository.event.Metadata)
	}
}

func TestWriterRejectsUnknownSensitiveAndNestedMetadata(t *testing.T) {
	writer := NewWriter(&fakeRepository{}, MetadataPolicy{"test.changed": {"reason": MetadataString}})
	for _, metadata := range []Metadata{{"password": "secret"}, {"reason": map[string]any{"nested": true}}, {"reason": ""}} {
		event := testEvent()
		event.Metadata = metadata
		if err := writer.Append(context.Background(), event); !errors.Is(err, ErrMetadataRejected) {
			t.Fatalf("metadata=%#v err=%v", metadata, err)
		}
	}
}

func TestWriterPropagatesMandatoryInsertFailure(t *testing.T) {
	insertErr := errors.New("insert failed")
	writer := NewWriter(&fakeRepository{err: insertErr}, MetadataPolicy{"test.changed": {"reason": MetadataString, "count": MetadataInteger}})
	if err := writer.Append(context.Background(), testEvent()); !errors.Is(err, insertErr) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriterValidatesActorShape(t *testing.T) {
	writer := NewWriter(&fakeRepository{}, MetadataPolicy{"test.changed": {"reason": MetadataString, "count": MetadataInteger}})
	event := testEvent()
	event.ActorUserID = nil
	if err := writer.Append(context.Background(), event); err == nil {
		t.Fatal("expected user actor validation error")
	}
	event = testEvent()
	event.ActorType = ActorSystem
	if err := writer.Append(context.Background(), event); err == nil {
		t.Fatal("expected system actor validation error")
	}
	event = testEvent()
	event.ActorUserID = nil
	sessionID := uuid.New()
	event.ActorSessionID = &sessionID
	if err := writer.Append(context.Background(), event); err == nil {
		t.Fatal("expected session without user validation error")
	}
	event = testEvent()
	event.ActorType = ActorSystem
	event.ActorUserID = nil
	if err := writer.Append(context.Background(), event); err != nil {
		t.Fatalf("valid system actor rejected: %v", err)
	}
}
