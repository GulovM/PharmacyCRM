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
	if len(r.processed) == 0 {
		return 0, errors.New("unexpected processed retention call")
	}
	value := r.processed[0]
	r.processed = r.processed[1:]
	return value, nil
}
func (r *fakeRetentionRepository) DeleteDeadLettersBefore(context.Context, time.Time, int) (int64, error) {
	r.calls = append(r.calls, "dead")
	if r.err != nil {
		return 0, r.err
	}
	if len(r.dead) == 0 {
		return 0, errors.New("unexpected dead-letter retention call")
	}
	value := r.dead[0]
	r.dead = r.dead[1:]
	return value, nil
}

type fakeRetentionTransactor struct{ repository *fakeRetentionRepository }

func (t fakeRetentionTransactor) WithinTransaction(ctx context.Context, fn func(context.Context, RetentionRepository) error) error {
	return fn(ctx, t.repository)
}

type retentionObserver struct {
	completed int
	failed    int
	stats     RetentionCycleStats
}

func (*retentionObserver) BatchDeleted(context.Context, string, int64) {}
func (o *retentionObserver) CycleFailed(context.Context, error)        { o.failed++ }
func (o *retentionObserver) CycleCompleted(_ context.Context, stats RetentionCycleStats) {
	o.completed++
	o.stats = stats
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

func TestRetentionCleanupUsesOneSharedBudgetAndAlternatesSingleBatchCycles(t *testing.T) {
	repository := &fakeRetentionRepository{processed: []int64{2, 2, 2, 0}, dead: []int64{2, 2, 0}}
	observer := &retentionObserver{}
	service, err := NewRetentionService(fakeRetentionTransactor{repository}, RetentionConfig{ProcessedFor: ProcessedRetentionPeriod, DeadLettersFor: DeadLetterRetentionPeriod, Interval: time.Hour, BatchSize: 2, MaxBatchesPerCycle: 5, MaxCycleDuration: time.Second}, observer)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cleanup(context.Background()); err != nil || len(repository.calls) > 5 || observer.stats.Batches > 5 {
		t.Fatalf("err=%v calls=%v stats=%#v", err, repository.calls, observer.stats)
	}
	repository = &fakeRetentionRepository{processed: []int64{0}, dead: []int64{0}}
	service, _ = NewRetentionService(fakeRetentionTransactor{repository}, RetentionConfig{ProcessedFor: ProcessedRetentionPeriod, DeadLettersFor: DeadLetterRetentionPeriod, Interval: time.Hour, BatchSize: 2, MaxBatchesPerCycle: 1, MaxCycleDuration: time.Second}, nil)
	if err := service.Cleanup(context.Background()); err != nil || len(repository.calls) != 1 || repository.calls[0] != "processed" {
		t.Fatalf("first single-budget cycle=%v err=%v", repository.calls, err)
	}
	repository.processed = []int64{0}
	repository.dead = []int64{0}
	if err := service.Cleanup(context.Background()); err != nil || len(repository.calls) != 2 || repository.calls[1] != "dead" {
		t.Fatalf("second single-budget cycle=%v err=%v", repository.calls, err)
	}
}

type blockingRetentionRepository struct{ cancelled bool }

func (r *blockingRetentionRepository) DeleteProcessedBefore(ctx context.Context, _ time.Time, _ int) (int64, error) {
	<-ctx.Done()
	r.cancelled = true
	return 0, ctx.Err()
}
func (*blockingRetentionRepository) DeleteDeadLettersBefore(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func TestRetentionCleanupTreatsInternalDeadlineAsLimitedWork(t *testing.T) {
	repository := &blockingRetentionRepository{}
	observer := &retentionObserver{}
	service, err := NewRetentionService(blockingRetentionTransactor{repository}, RetentionConfig{ProcessedFor: ProcessedRetentionPeriod, DeadLettersFor: DeadLetterRetentionPeriod, Interval: time.Hour, BatchSize: 1, MaxBatchesPerCycle: 1, MaxCycleDuration: 10 * time.Millisecond}, observer)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cleanup(context.Background()); err != nil || !repository.cancelled || !observer.stats.Limited || observer.completed != 1 {
		t.Fatalf("err=%v cancelled=%t stats=%#v completed=%d", err, repository.cancelled, observer.stats, observer.completed)
	}
}

type blockingRetentionTransactor struct{ repository *blockingRetentionRepository }

func (t blockingRetentionTransactor) WithinTransaction(ctx context.Context, fn func(context.Context, RetentionRepository) error) error {
	return fn(ctx, t.repository)
}
