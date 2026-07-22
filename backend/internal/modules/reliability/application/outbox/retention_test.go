package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRetentionRepository struct {
	processed []int64
	dead      []int64
	err       error
	calls     []string
}

func (r *fakeRetentionRepository) DeleteProcessedBefore(context.Context, time.Time, int) (int64, error) {
	r.calls = append(r.calls, "processed")
	if r.err != nil {
		return 0, r.err
	}
	value := r.processed[0]
	r.processed = r.processed[1:]
	return value, nil
}
func (r *fakeRetentionRepository) DeleteDeadLettersBefore(context.Context, time.Time, int) (int64, error) {
	r.calls = append(r.calls, "dead")
	value := r.dead[0]
	r.dead = r.dead[1:]
	return value, nil
}

type fakeRetentionTransactor struct{ repository *fakeRetentionRepository }

func (t fakeRetentionTransactor) WithinTransaction(ctx context.Context, fn func(context.Context, RetentionRepository) error) error {
	return fn(ctx, t.repository)
}

func TestRetentionCleanupUsesBoundedBatchesAndStopsOnError(t *testing.T) {
	repository := &fakeRetentionRepository{processed: []int64{2, 2, 1}, dead: []int64{0}}
	service, err := NewRetentionService(fakeRetentionTransactor{repository}, RetentionConfig{ProcessedFor: ProcessedRetentionPeriod, DeadLettersFor: DeadLetterRetentionPeriod, Interval: time.Hour, BatchSize: 2, MaxBatchesPerCycle: 10, MaxCycleDuration: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(repository.calls); got != 4 {
		t.Fatalf("batch calls=%d", got)
	}

	batchErr := errors.New("delete failed")
	repository = &fakeRetentionRepository{processed: []int64{1}, dead: []int64{0}, err: batchErr}
	service, _ = NewRetentionService(fakeRetentionTransactor{repository}, RetentionConfig{ProcessedFor: ProcessedRetentionPeriod, DeadLettersFor: DeadLetterRetentionPeriod, Interval: time.Hour, BatchSize: 2, MaxBatchesPerCycle: 10, MaxCycleDuration: time.Second}, nil)
	if err := service.Cleanup(context.Background()); !errors.Is(err, batchErr) {
		t.Fatalf("error=%v", err)
	}
	if len(repository.calls) != 1 {
		t.Fatalf("cleanup continued after error: %#v", repository.calls)
	}
}

func TestRetentionCleanupHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repository := &fakeRetentionRepository{processed: []int64{0}, dead: []int64{0}}
	service, _ := NewRetentionService(fakeRetentionTransactor{repository}, RetentionConfig{ProcessedFor: ProcessedRetentionPeriod, DeadLettersFor: DeadLetterRetentionPeriod, Interval: time.Hour, BatchSize: 2, MaxBatchesPerCycle: 10, MaxCycleDuration: time.Second}, nil)
	if err := service.Cleanup(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if len(repository.calls) != 0 {
		t.Fatal("repository called after cancellation")
	}
}

func TestRetentionCleanupLimitsEachCycleAndProcessesBothStatuses(t *testing.T) {
	repository := &fakeRetentionRepository{processed: []int64{2, 2, 2}, dead: []int64{2, 2, 2}}
	service, err := NewRetentionService(fakeRetentionTransactor{repository}, RetentionConfig{ProcessedFor: ProcessedRetentionPeriod, DeadLettersFor: DeadLetterRetentionPeriod, Interval: time.Hour, BatchSize: 2, MaxBatchesPerCycle: 2, MaxCycleDuration: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repository.calls) != 2 || repository.calls[0] != "processed" || repository.calls[1] != "dead" {
		t.Fatalf("retention was not fair and bounded: %#v", repository.calls)
	}
}
