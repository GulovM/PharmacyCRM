package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestOutboxClaimErrorClassifier(t *testing.T) {
	classifier := OutboxClaimErrorClassifier{}
	for name, err := range map[string]error{
		"serialization":   fmt.Errorf("claim failed: %w", &pgconn.PgError{Code: "40001"}),
		"deadlock":        fmt.Errorf("claim failed: %w", &pgconn.PgError{Code: "40P01"}),
		"acquire timeout": fmt.Errorf("claim failed: %w", context.DeadlineExceeded),
	} {
		t.Run(name, func(t *testing.T) {
			if !classifier.IsTransientClaimError(err) {
				t.Fatalf("expected transient classification for %v", err)
			}
		})
	}
	if classifier.IsTransientClaimError(errors.New("relation outbox_events does not exist")) {
		t.Fatal("unknown repository state must remain fatal")
	}
}
