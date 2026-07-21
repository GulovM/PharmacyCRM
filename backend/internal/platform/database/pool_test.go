package database

import (
	"strings"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
)

func postgresConfig() config.PostgresConfig {
	return config.PostgresConfig{RuntimeDSN: "postgres://runtime:runtime-secret@localhost:5432/pharmacy", MigrationDSN: "postgres://migrator:migration-secret@localhost:5432/pharmacy", MinConnections: 2, MaxConnections: 8, AcquireTimeout: 3 * time.Second, MaxConnectionLife: time.Hour, MaxConnectionIdle: 5 * time.Minute, HealthCheckPeriod: time.Minute, ConnectionCapacity: 10}
}
func TestBuildPoolConfigAppliesPoolBudget(t *testing.T) {
	cfg := postgresConfig()
	poolConfig, err := BuildPoolConfig(cfg, cfg.RuntimeDSN)
	if err != nil {
		t.Fatal(err)
	}
	if poolConfig.MinConns != 2 || poolConfig.MaxConns != 8 || poolConfig.MaxConnLifetime != time.Hour || poolConfig.MaxConnIdleTime != 5*time.Minute || poolConfig.HealthCheckPeriod != time.Minute || poolConfig.ConnConfig.ConnectTimeout != 3*time.Second {
		t.Fatalf("unexpected pool configuration: %#v", poolConfig)
	}
}
func TestBuildPoolConfigDoesNotLeakDSN(t *testing.T) {
	_, err := BuildPoolConfig(postgresConfig(), "postgres://user:super-secret@%")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("dsn leaked: %v", err)
	}
}
