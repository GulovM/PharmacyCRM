package httpserver

import (
	"context"
	"errors"
	"sync/atomic"
)

// DatabasePinger is the narrow dependency needed by readiness.
type DatabasePinger interface{ Ping(context.Context) error }

// Readiness separates process liveness from traffic eligibility.
type Readiness struct {
	database        DatabasePinger
	draining        atomic.Bool
	startupComplete atomic.Bool
}

func NewReadiness(database DatabasePinger) *Readiness {
	readiness := &Readiness{database: database}
	readiness.startupComplete.Store(true)
	return readiness
}
func (r *Readiness) SetDraining() { r.draining.Store(true) }
func (r *Readiness) Ready(ctx context.Context) error {
	if !r.startupComplete.Load() || r.draining.Load() {
		return errors.New("not ready")
	}
	if r.database == nil {
		return errors.New("database is not initialized")
	}
	return r.database.Ping(ctx)
}
