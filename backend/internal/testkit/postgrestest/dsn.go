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
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn != "" {
		return dsn
	}
	if strings.EqualFold(os.Getenv("CI_INTEGRATION_REQUIRED"), "true") {
		t.Fatal("POSTGRES_TEST_DSN is required by the CI integration gate")
	}
	t.Skip("POSTGRES_TEST_DSN is not set")
	return ""
}
