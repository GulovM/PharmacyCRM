package httpserver

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
)

// DatabasePinger is the narrow dependency needed by readiness.
type DatabasePinger interface{ Ping(context.Context) error }

// Check is a critical readiness dependency such as schema or worker protocol
// compatibility. Checks must not disclose infrastructure details to callers.
type Check func(context.Context) error

// Readiness separates process liveness from traffic eligibility.
type Readiness struct {
	database        DatabasePinger
	schema          Check
	workerProtocol  Check
	criticalInit    Check
	draining        atomic.Bool
	startupComplete atomic.Bool
}

func NewReadiness(database DatabasePinger, schema, workerProtocol, criticalInit Check) *Readiness {
	return &Readiness{database: database, schema: schema, workerProtocol: workerProtocol, criticalInit: criticalInit}
}

// MarkStartupComplete is called only after all composition-root initialization
// has succeeded. A new readiness instance is deliberately not ready.
func (r *Readiness) MarkStartupComplete() { r.startupComplete.Store(true) }
func (r *Readiness) SetDraining()         { r.draining.Store(true) }

func (r *Readiness) Ready(ctx context.Context) error {
	if !r.startupComplete.Load() || r.draining.Load() {
		return errors.New("not ready")
	}
	if r.database == nil {
		return errors.New("database is not initialized")
	}
	if err := r.database.Ping(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	for _, check := range []Check{r.schema, r.workerProtocol, r.criticalInit} {
		if check != nil {
			if err := check(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
