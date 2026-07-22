package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	MaxRetentionBatchSize     = 1000
	ProcessedRetentionPeriod  = 30 * 24 * time.Hour
	DeadLetterRetentionPeriod = 180 * 24 * time.Hour
)

type RetentionRepository interface {
	DeleteProcessedBefore(context.Context, time.Time, int) (int64, error)
	DeleteDeadLettersBefore(context.Context, time.Time, int) (int64, error)
}

type RetentionTransactor interface {
	WithinTransaction(context.Context, func(context.Context, RetentionRepository) error) error
}

type RetentionConfig struct {
	ProcessedFor       time.Duration
	DeadLettersFor     time.Duration
	Interval           time.Duration
	BatchSize          int
	MaxBatchesPerCycle int
	MaxCycleDuration   time.Duration
}

type RetentionCycleStats struct {
	ProcessedDeleted  int64
	DeadLetterDeleted int64
	Batches           int
	Limited           bool
	Duration          time.Duration
}

type RetentionObserver interface {
	BatchDeleted(context.Context, string, int64)
	CycleFailed(context.Context, error)
	CycleCompleted(context.Context, RetentionCycleStats)
}

type noopRetentionObserver struct{}

func (noopRetentionObserver) BatchDeleted(context.Context, string, int64)         {}
func (noopRetentionObserver) CycleFailed(context.Context, error)                  {}
func (noopRetentionObserver) CycleCompleted(context.Context, RetentionCycleStats) {}

type RetentionService struct {
	transactor RetentionTransactor
	config     RetentionConfig
	observer   RetentionObserver
	now        func() time.Time
}

func NewRetentionService(transactor RetentionTransactor, config RetentionConfig, observer RetentionObserver) (*RetentionService, error) {
	if transactor == nil || config.ProcessedFor != ProcessedRetentionPeriod || config.DeadLettersFor != DeadLetterRetentionPeriod || config.Interval <= 0 || config.BatchSize < 1 || config.BatchSize > MaxRetentionBatchSize || config.MaxBatchesPerCycle < 1 || config.MaxCycleDuration <= 0 {
		return nil, errors.Join(ErrInvalidEvent, errors.New("invalid retention configuration"))
	}
	if observer == nil {
		observer = noopRetentionObserver{}
	}
	return &RetentionService{transactor: transactor, config: config, observer: observer, now: time.Now}, nil
}

func (s *RetentionService) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := s.Cleanup(ctx); err != nil && ctx.Err() == nil {
			s.observer.CycleFailed(ctx, err)
		}
		timer := time.NewTimer(s.config.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (s *RetentionService) Cleanup(ctx context.Context) error {
	startedAt := s.now()
	stats := RetentionCycleStats{}
	defer func() {
		stats.Duration = s.now().Sub(startedAt)
		s.observer.CycleCompleted(ctx, stats)
	}()
	perStatusBudget := (s.config.MaxBatchesPerCycle + 1) / 2
	processed, limited, err := s.deleteBatches(ctx, "PROCESSED", startedAt, startedAt.Add(-s.config.ProcessedFor), perStatusBudget, func(ctx context.Context, repository RetentionRepository, before time.Time, limit int) (int64, error) {
		return repository.DeleteProcessedBefore(ctx, before, limit)
	})
	stats.ProcessedDeleted += processed.deleted
	stats.Batches += processed.batches
	stats.Limited = limited
	if err != nil {
		return fmt.Errorf("delete processed outbox events: %w", err)
	}
	dead, limited, err := s.deleteBatches(ctx, "DEAD_LETTER", startedAt, startedAt.Add(-s.config.DeadLettersFor), perStatusBudget, func(ctx context.Context, repository RetentionRepository, before time.Time, limit int) (int64, error) {
		return repository.DeleteDeadLettersBefore(ctx, before, limit)
	})
	stats.DeadLetterDeleted += dead.deleted
	stats.Batches += dead.batches
	stats.Limited = stats.Limited || limited
	if err != nil {
		return fmt.Errorf("delete dead-letter outbox events: %w", err)
	}
	return nil
}

type retentionDelete func(context.Context, RetentionRepository, time.Time, int) (int64, error)

type retentionBatchResult struct {
	deleted int64
	batches int
}

func (s *RetentionService) deleteBatches(ctx context.Context, status string, startedAt, before time.Time, budget int, deleteBatch retentionDelete) (retentionBatchResult, bool, error) {
	result := retentionBatchResult{}
	for result.batches < budget {
		if err := ctx.Err(); err != nil {
			return result, false, err
		}
		if s.now().Sub(startedAt) >= s.config.MaxCycleDuration {
			return result, true, nil
		}
		var deleted int64
		err := s.transactor.WithinTransaction(ctx, func(ctx context.Context, repository RetentionRepository) error {
			var err error
			deleted, err = deleteBatch(ctx, repository, before, s.config.BatchSize)
			return err
		})
		if err != nil {
			return result, false, err
		}
		if deleted < 0 || deleted > int64(s.config.BatchSize) {
			return result, false, errors.New("retention repository returned an invalid delete count")
		}
		result.batches++
		result.deleted += deleted
		s.observer.BatchDeleted(ctx, status, deleted)
		if deleted < int64(s.config.BatchSize) {
			return result, false, nil
		}
	}
	return result, true, nil
}
