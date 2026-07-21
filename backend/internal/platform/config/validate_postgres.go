package config

import (
	"net/url"
	"strings"
)

func validateRuntimePostgres(c RuntimePostgresConfig) error {
	return validatePostgres(c.DSN, c.PoolConfig, "runtime")
}
func validateMigrationPostgres(c MigrationPostgresConfig) error {
	return validatePostgres(c.DSN, c.PoolConfig, "migration")
}
func validatePostgres(dsn string, c PoolConfig, purpose string) error {
	if err := validatePostgresDSN(dsn); err != nil {
		return invalid("postgres " + purpose + " dsn is invalid")
	}
	if c.MinConnections < 0 || c.MaxConnections < 1 || c.MinConnections > c.MaxConnections {
		return invalid("postgres pool bounds are invalid")
	}
	if c.ConnectionCapacity < c.MaxConnections {
		return invalid("postgres pool exceeds configured connection capacity")
	}
	if c.AcquireTimeout <= 0 || c.MaxConnectionLife <= 0 || c.MaxConnectionIdle <= 0 || c.HealthCheckPeriod <= 0 {
		return invalid("postgres timeouts must be positive")
	}
	return nil
}
func validatePostgresDSN(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" || u.User == nil || strings.TrimPrefix(u.Path, "/") == "" {
		return invalid("dsn")
	}
	return nil
}
