// Package postgrestest contains shared contracts for PostgreSQL integration tests.
package postgrestest

import (
	"os"
	"strings"
	"testing"
)

// DSN skips database tests during ordinary local runs, but turns a missing DSN
// into a hard failure in the mandatory CI integration gate.
func DSN(t testing.TB) string {
	return requiredDSN(t, "POSTGRES_TEST_DSN")
}

// RuntimeDSN returns the least-privilege API connection used by direct-SQL
// invariant tests.
func RuntimeDSN(t testing.TB) string {
	return requiredDSN(t, "POSTGRES_API_RUNTIME_DSN")
}

// WorkerRuntimeDSN returns the dedicated least-privilege worker connection used
// to prove that management-plane capabilities are unavailable to background jobs.
func WorkerRuntimeDSN(t testing.TB) string {
	return requiredDSN(t, "POSTGRES_WORKER_RUNTIME_DSN")
}

func requiredDSN(t testing.TB, variable string) string {
	t.Helper()
	dsn := os.Getenv(variable)
	if dsn != "" {
		return dsn
	}
	if strings.EqualFold(os.Getenv("CI_INTEGRATION_REQUIRED"), "true") {
		t.Fatalf("%s is required by the CI integration gate", variable)
	}
	t.Skipf("%s is not set", variable)
	return ""
}
