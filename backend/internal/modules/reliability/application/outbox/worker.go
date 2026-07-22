package outbox

import (
	"context"
	"errors"
	"math/rand/v2"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	workerRetryBase = 2 * time.Second
	workerRetryCap  = 15 * time.Minute
	claimRetryBase  = 100 * time.Millisecond
	claimRetryCap   = 5 * time.Second
)

type Transactor interface {
	WithinTransaction(context.Context, func(context.Context, Repository) error) error
}

type WorkerConfig struct {
	Owner         string
	Concurrency   int
	MaxClaim      int
	PollInterval  time.Duration
	LeaseDuration time.Duration
	DrainTimeout  time.Duration
}

type Worker struct {
	transactor      Transactor
	handlers        map[EventKey]Handler
	config          WorkerConfig
	now             func() time.Time
	retryDelay      func(int16) time.Duration
	claimRetryDelay func(int) time.Duration
	claimErrors     ClaimErrorClassifier
	protocols       []EventKey
	observer        Observer
}

type ClaimErrorClassifier interface {
	IsTransientClaimError(error) bool
}

type ClaimErrorClassifierFunc func(error) bool

func (f ClaimErrorClassifierFunc) IsTransientClaimError(err error) bool { return f(err) }

type Observer interface {
	ClaimFailed(context.Context, error, bool, time.Duration)
	FinalizationFailed(context.Context, Lease, error)
	StaleLease(context.Context, Lease)
	DeadLettered(context.Context, Lease, string)
}

type noopObserver struct{}

func (noopObserver) ClaimFailed(context.Context, error, bool, time.Duration) {}
func (noopObserver) FinalizationFailed(context.Context, Lease, error)        {}
func (noopObserver) StaleLease(context.Context, Lease)                       {}
func (noopObserver) DeadLettered(context.Context, Lease, string)             {}

func NewWorker(transactor Transactor, handlers map[EventKey]Handler, config WorkerConfig, observer Observer, claimErrors ClaimErrorClassifier) (*Worker, error) {
	if transactor == nil || claimErrors == nil || strings.TrimSpace(config.Owner) == "" || config.Concurrency < 1 || config.MaxClaim < 1 || config.MaxClaim > 100 || config.PollInterval <= 0 || config.LeaseDuration <= 0 || config.DrainTimeout <= 0 {
		return nil, errors.Join(ErrInvalidEvent, errors.New("invalid worker configuration"))
	}
	copyHandlers := make(map[EventKey]Handler, len(handlers))
	protocols := make([]EventKey, 0, len(handlers))
	for key, handler := range handlers {
		if key.Name == "" || key.Version < 1 || handler == nil {
			return nil, errors.Join(ErrInvalidEvent, errors.New("invalid handler registration"))
		}
		copyHandlers[key] = handler
		protocols = append(protocols, key)
	}
	sort.Slice(protocols, func(i, j int) bool {
		if protocols[i].Name == protocols[j].Name {
			return protocols[i].Version < protocols[j].Version
		}
		return protocols[i].Name < protocols[j].Name
	})
	if observer == nil {
		observer = noopObserver{}
	}
	return &Worker{transactor: transactor, handlers: copyHandlers, protocols: protocols, config: config, now: time.Now, retryDelay: outboxRetryDelay, claimRetryDelay: pollingRetryDelay, claimErrors: claimErrors, observer: observer}, nil
}

func (w *Worker) SupportsProtocol(key EventKey) bool {
	_, ok := w.handlers[key]
	return ok
}

func (w *Worker) ValidateProtocols(required []EventKey) error {
	for _, key := range required {
		if !w.SupportsProtocol(key) {
			return errors.Join(ErrInvalidEvent, errors.New("unsupported outbox event protocol"))
		}
	}
	return nil
}

func (w *Worker) Run(ctx context.Context) error {
	processingCtx, stopProcessing := context.WithCancel(context.WithoutCancel(ctx))
	defer stopProcessing()
	semaphore := make(chan struct{}, w.config.Concurrency)
	var workers sync.WaitGroup
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	failedClaims := 0

	for {
		if err := ctx.Err(); err != nil {
			return w.shutdown(&workers, stopProcessing, nil)
		}
		available := cap(semaphore) - len(semaphore)
		if available > 0 {
			limit := min(available, w.config.MaxClaim)
			leases, err := w.claim(ctx, limit)
			if err != nil {
				if !w.claimErrors.IsTransientClaimError(err) {
					w.observer.ClaimFailed(ctx, err, false, 0)
					return w.shutdown(&workers, stopProcessing, err)
				}
				failedClaims++
				delay := w.claimRetryDelay(failedClaims)
				w.observer.ClaimFailed(ctx, err, true, delay)
				if !waitForContext(ctx, delay) {
					return w.shutdown(&workers, stopProcessing, nil)
				}
				continue
			}
			failedClaims = 0
			for _, lease := range leases {
				semaphore <- struct{}{}
				workers.Add(1)
				go func(lease Lease) {
					defer workers.Done()
					defer func() { <-semaphore }()
					w.process(processingCtx, lease)
				}(lease)
			}
			if len(leases) > 0 {
				continue
			}
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

func (w *Worker) shutdown(workers *sync.WaitGroup, cancel context.CancelFunc, primary error) error {
	drainErr := waitForDrain(workers, cancel, w.config.DrainTimeout)
	return errors.Join(primary, drainErr)
}

func (w *Worker) claim(ctx context.Context, limit int) ([]Lease, error) {
	var leases []Lease
	err := w.transactor.WithinTransaction(ctx, func(ctx context.Context, repository Repository) error {
		var err error
		leases, err = repository.ClaimBatch(ctx, ClaimRequest{Owner: w.config.Owner, Limit: limit, LeaseDuration: w.config.LeaseDuration, Now: w.now(), Protocols: w.protocols})
		return err
	})
	return leases, err
}

func (w *Worker) process(ctx context.Context, lease Lease) {
	defer func() {
		if recover() != nil {
			w.finish(ctx, lease, Failure{Code: "HANDLER_PANIC", Retryable: true})
		}
	}()
	handler, ok := w.handlers[lease.EventKey]
	if !ok {
		w.finish(ctx, lease, Failure{Code: "UNSUPPORTED_EVENT_PROTOCOL", Retryable: false})
		return
	}
	if err := handler.Handle(ctx, lease.Event); err != nil {
		var deliveryError *DeliveryError
		if errors.As(err, &deliveryError) && failureCodePattern.MatchString(deliveryError.Code) {
			w.finish(ctx, lease, Failure{Code: deliveryError.Code, Retryable: deliveryError.Retryable})
		} else {
			w.finish(ctx, lease, Failure{Code: "HANDLER_FAILED", Retryable: true})
		}
		return
	}
	err := w.transactor.WithinTransaction(ctx, func(ctx context.Context, repository Repository) error {
		return repository.MarkProcessed(ctx, lease, w.now())
	})
	w.observeFinalization(ctx, lease, err)
}

func (w *Worker) finish(ctx context.Context, lease Lease, failure Failure) {
	now := w.now()
	availableAt := now
	if failure.Retryable && lease.Attempt < lease.MaxAttempts {
		availableAt = now.Add(w.retryDelay(lease.Attempt))
	}
	err := w.transactor.WithinTransaction(ctx, func(ctx context.Context, repository Repository) error {
		return repository.MarkFailed(ctx, lease, failure, now, availableAt)
	})
	if w.observeFinalization(ctx, lease, err) && (!failure.Retryable || lease.Attempt >= lease.MaxAttempts) {
		w.observer.DeadLettered(ctx, lease, failure.Code)
	}
}

func (w *Worker) observeFinalization(ctx context.Context, lease Lease, err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, ErrStaleLease) {
		w.observer.StaleLease(ctx, lease)
	} else {
		w.observer.FinalizationFailed(ctx, lease, err)
	}
	return false
}

type DeliveryError struct {
	Code      string
	Retryable bool
}

func (e *DeliveryError) Error() string { return e.Code }

var failureCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,99}$`)

func NewDeliveryError(code string, retryable bool) error {
	if !failureCodePattern.MatchString(code) {
		return errors.Join(ErrInvalidEvent, errors.New("invalid delivery error code"))
	}
	return &DeliveryError{Code: code, Retryable: retryable}
}

func outboxRetryDelay(attempt int16) time.Duration {
	limit := workerRetryBase
	for current := int16(1); current < attempt && limit < workerRetryCap; current++ {
		limit *= 2
	}
	if limit > workerRetryCap {
		limit = workerRetryCap
	}
	return time.Duration(rand.Int64N(int64(limit) + 1))
}

func pollingRetryDelay(failedAttempt int) time.Duration {
	limit := claimRetryBase
	for current := 1; current < failedAttempt && limit < claimRetryCap; current++ {
		limit *= 2
	}
	if limit > claimRetryCap {
		limit = claimRetryCap
	}
	return time.Duration(rand.Int64N(int64(limit) + 1))
}

func waitForContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func waitForDrain(workers *sync.WaitGroup, cancel context.CancelFunc, timeout time.Duration) error {
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		cancel()
		return context.DeadlineExceeded
	}
}
