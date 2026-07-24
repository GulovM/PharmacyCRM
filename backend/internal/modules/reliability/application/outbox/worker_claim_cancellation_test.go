package outbox

import (
	"context"
	"testing"
	"time"
)

type cancellationClaimTransactor struct {
	started chan struct{}
}

func (transactor cancellationClaimTransactor) WithinTransaction(ctx context.Context, _ func(context.Context, Repository) error) error {
	close(transactor.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestCancellationDuringClaimIsGraceful(t *testing.T) {
	started := make(chan struct{})
	worker, err := NewWorker(
		cancellationClaimTransactor{started: started},
		map[EventKey]Handler{},
		WorkerConfig{
			Owner:         "worker-cancel-claim",
			Concurrency:   1,
			MaxClaim:      1,
			PollInterval:  time.Second,
			LeaseDuration: time.Minute,
			DrainTimeout:  time.Second,
		},
		nil,
		ClaimErrorClassifierFunc(func(error) bool { return false }),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	<-started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("claim cancellation returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after claim cancellation")
	}
}
