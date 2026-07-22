package migration

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	embeddedmigrations "github.com/GulovM/PharmacyCRM/backend/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUpgradeFromE1Integration(t *testing.T) {
	adminDSN := os.Getenv("POSTGRES_ADMIN_TEST_DSN")
	migrationDSN := os.Getenv("POSTGRES_TEST_DSN")
	if adminDSN == "" || migrationDSN == "" {
		if os.Getenv("CI_INTEGRATION_REQUIRED") == "true" {
			t.Fatal("POSTGRES_ADMIN_TEST_DSN and POSTGRES_TEST_DSN are required")
		}
		t.Skip("PostgreSQL admin and migration test DSNs are not set")
	}
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminPool.Close)

	migrationConfig, err := pgxpool.ParseConfig(migrationDSN)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := "pharmacycrm_e1_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{databaseName}.Sanitize()
	owner := pgx.Identifier{migrationConfig.ConnConfig.User}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+identifier+" OWNER "+owner+" TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	})
	parsedMigrationDSN, err := url.Parse(migrationDSN)
	if err != nil || parsedMigrationDSN.Scheme == "" || parsedMigrationDSN.Host == "" {
		t.Fatal("POSTGRES_TEST_DSN must be a PostgreSQL URL")
	}
	parsedMigrationDSN.Path = "/" + databaseName
	isolatedDSN := parsedMigrationDSN.String()

	loaded, err := Load(embeddedmigrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 15 || loaded[0].Version != 1 || loaded[0].Name != "schema_metadata" {
		t.Fatalf("unexpected migration set: %#v", loaded)
	}
	rawPool, err := pgxpool.New(ctx, isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawPool.Exec(ctx, loaded[0].SQL); err != nil {
		rawPool.Close()
		t.Fatal(err)
	}
	if _, err := rawPool.Exec(ctx, `CREATE TABLE pharmacycrm_schema_migrations (
		version bigint PRIMARY KEY, name text NOT NULL, checksum text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		rawPool.Close()
		t.Fatal(err)
	}
	if _, err := rawPool.Exec(ctx, "INSERT INTO pharmacycrm_schema_migrations (version,name,checksum) VALUES ($1,$2,$3)", loaded[0].Version, loaded[0].Name, loaded[0].Checksum); err != nil {
		rawPool.Close()
		t.Fatal(err)
	}
	rawPool.Close()

	pool, err := database.NewMigration(ctx, config.MigrationPostgresConfig{
		DSN: isolatedDSN,
		PoolConfig: config.PoolConfig{
			MinConnections: 1, MaxConnections: 2, AcquireTimeout: 5 * time.Second,
			MaxConnectionLife: time.Minute, MaxConnectionIdle: time.Minute, HealthCheckPeriod: time.Minute,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	result, err := Run(ctx, pool, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 15 || len(result.Applied) != 14 || result.Applied[0] != 2 || result.Applied[len(result.Applied)-1] != 15 {
		t.Fatalf("unexpected upgrade result: %#v", result)
	}
	replayed, err := Run(ctx, pool, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.SchemaVersion != 15 || len(replayed.Applied) != 0 {
		t.Fatalf("migrations were unexpectedly replayed: %#v", replayed)
	}

	corrupted := append([]Migration(nil), loaded...)
	corrupted[0].Checksum = strings.Repeat("0", 64)
	if _, err := Run(ctx, pool, corrupted); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}
