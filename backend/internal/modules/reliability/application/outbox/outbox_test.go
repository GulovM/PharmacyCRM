package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepository struct {
	mu              sync.Mutex
	appended        Event
	processed       int
	failed          []Failure
	finalizationErr error
}

func (f *fakeRepository) Append(_ context.Context, event Event) error {
	f.appended = event
	return nil
}
func (f *fakeRepository) ClaimBatch(context.Context, ClaimRequest) ([]Lease, error) { return nil, nil }
func (f *fakeRepository) MarkProcessed(context.Context, Lease, time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.processed++
	return f.finalizationErr
}
func (f *fakeRepository) MarkFailed(_ context.Context, _ Lease, failure Failure, _, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, failure)
	return f.finalizationErr
}
func (f *fakeRepository) ReplayDeadLetter(context.Context, uuid.UUID, time.Time) error { return nil }

type fakeTransactor struct{ repository Repository }

func (f fakeTransactor) WithinTransaction(ctx context.Context, fn func(context.Context, Repository) error) error {
	return fn(ctx, f.repository)
}

type handlerFunc func(context.Context, Event) error

func (f handlerFunc) Handle(ctx context.Context, event Event) error { return f(ctx, event) }

type recordingObserver struct {
	stale, finalization, dead int
}

func (o *recordingObserver) FinalizationFailed(context.Context, Lease, error) { o.finalization++ }
func (o *recordingObserver) StaleLease(context.Context, Lease)                { o.stale++ }
func (o *recordingObserver) DeadLettered(context.Context, Lease, string)      { o.dead++ }

func validEvent() Event {
	return Event{
		ID: uuid.New(), EventKey: EventKey{Name: "inventory.changed", Version: 1},
		AggregateType: "stock_lot", AggregateID: uuid.New(), PartitionKey: "pharmacy-1",
		DeduplicationKey: uuid.NewString(), Payload: []byte(`{"quantity_base_units":10}`),
		Headers: map[string]string{"correlation_id": uuid.NewString()}, OccurredAt: time.Now(),
	}
}

func testWorker(t *testing.T, repository *fakeRepository, handler Handler, observer Observer) *Worker {
	t.Helper()
	key := EventKey{Name: "inventory.changed", Version: 1}
	worker, err := NewWorker(fakeTransactor{repository}, map[EventKey]Handler{key: handler}, WorkerConfig{
		Owner: "worker-1", Concurrency: 1, MaxClaim: 1, PollInterval: time.Millisecond,
		LeaseDuration: time.Minute, DrainTimeout: time.Second,
	}, observer)
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return time.Unix(100, 0) }
	worker.retryDelay = func(int16) time.Duration { return time.Second }
	return worker
}

func TestWriterDefaultsAttemptsAndRejectsSecrets(t *testing.T) {
	repository := &fakeRepository{}
	writer := NewWriter(repository, map[EventKey]PayloadValidator{
		{Name: "inventory.changed", Version: 1}: PayloadValidatorFunc(func(json.RawMessage) error { return nil }),
	})
	event := validEvent()
	if err := writer.Append(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if repository.appended.MaxAttempts != DefaultMaxAttempts {
		t.Fatalf("max attempts = %d", repository.appended.MaxAttempts)
	}
	event = validEvent()
	event.Payload = []byte(`{"access_token":"secret"}`)
	if err := writer.Append(context.Background(), event); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("expected secret rejection, got %v", err)
	}
	unknown := validEvent()
	unknown.EventKey.Version = 2
	if err := writer.Append(context.Background(), unknown); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("expected unregistered protocol rejection, got %v", err)
	}
}

func TestWorkerProcessesAndDeadLettersPoisonEvent(t *testing.T) {
	repository := &fakeRepository{}
	observer := &recordingObserver{}
	worker := testWorker(t, repository, handlerFunc(func(context.Context, Event) error {
		return &DeliveryError{Code: "POISON_EVENT", Retryable: false}
	}), observer)
	lease := Lease{Event: validEvent(), Token: uuid.New(), Generation: 1, Attempt: 1}
	lease.MaxAttempts = 8
	worker.process(context.Background(), lease)
	if len(repository.failed) != 1 || repository.failed[0].Code != "POISON_EVENT" || observer.dead != 1 {
		t.Fatalf("failed=%#v dead=%d", repository.failed, observer.dead)
	}
}

func TestWorkerRecoversHandlerPanicAndObservesStaleLease(t *testing.T) {
	repository := &fakeRepository{finalizationErr: ErrStaleLease}
	observer := &recordingObserver{}
	worker := testWorker(t, repository, handlerFunc(func(context.Context, Event) error { panic("boom") }), observer)
	lease := Lease{Event: validEvent(), Token: uuid.New(), Generation: 1, Attempt: 1}
	lease.MaxAttempts = 8
	worker.process(context.Background(), lease)
	if len(repository.failed) != 1 || repository.failed[0].Code != "HANDLER_PANIC" || observer.stale != 1 {
		t.Fatalf("failed=%#v stale=%d", repository.failed, observer.stale)
	}
}

func TestProtocolMismatchFailsReadiness(t *testing.T) {
	worker := testWorker(t, &fakeRepository{}, handlerFunc(func(context.Context, Event) error { return nil }), nil)
	if err := worker.ValidateProtocols([]EventKey{{Name: "inventory.changed", Version: 2}}); err == nil {
		t.Fatal("expected unsupported protocol")
	}
}

func TestDrainTimeoutIsBounded(t *testing.T) {
	var workers sync.WaitGroup
	workers.Add(1)
	started := time.Now()
	if err := waitForDrain(&workers, func() {}, 10*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("drain exceeded bound: %s", elapsed)
	}
	workers.Done()
}
