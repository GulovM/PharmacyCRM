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
	if len(loaded) != 24 || loaded[0].Version != 1 || loaded[0].Name != "schema_metadata" {
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
	if result.SchemaVersion != 24 || len(result.Applied) != 23 || result.Applied[0] != 2 || result.Applied[len(result.Applied)-1] != 24 {
		t.Fatalf("unexpected upgrade result: %#v", result)
	}
	replayed, err := Run(ctx, pool, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.SchemaVersion != 24 || len(replayed.Applied) != 0 {
		t.Fatalf("migrations were unexpectedly replayed: %#v", replayed)
	}

	verificationPool, err := pgxpool.New(ctx, isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verificationPool.Close)
	assertVerificationFailure := func(version int64, name string) {
		t.Helper()
		_, err := Run(ctx, pool, loaded)
		var verificationError *VerificationError
		if !errors.As(err, &verificationError) {
			t.Fatalf("expected verification error, got %v", err)
		}
		if verificationError.Version != version || verificationError.Name != name || !errors.Is(verificationError, ErrVerificationFailed) {
			t.Fatalf("unexpected verification error: %#v", verificationError)
		}
	}
	if _, err := verificationPool.Exec(ctx, `DROP INDEX uq_user_single_active_role`); err != nil {
		t.Fatal(err)
	}
	assertVerificationFailure(3, "identity")
	if _, err := verificationPool.Exec(ctx, `CREATE UNIQUE INDEX uq_user_single_active_role ON user_roles(user_id) WHERE revoked_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := verificationPool.Exec(ctx, `ALTER TABLE outbox_events DROP CONSTRAINT chk_outbox_terminal`); err != nil {
		t.Fatal(err)
	}
	assertVerificationFailure(11, "outbox")
	if _, err := verificationPool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT chk_outbox_terminal CHECK ((status = 'PROCESSED' AND processed_at IS NOT NULL AND dead_lettered_at IS NULL) OR (status = 'DEAD_LETTER' AND dead_lettered_at IS NOT NULL AND processed_at IS NULL) OR (status IN ('PENDING', 'PROCESSING') AND processed_at IS NULL AND dead_lettered_at IS NULL))`); err != nil {
		t.Fatal(err)
	}
	if _, err := verificationPool.Exec(ctx, `REVOKE INSERT ON inventory_movements FROM pharmacycrm_runtime`); err != nil {
		t.Fatal(err)
	}
	assertVerificationFailure(17, "runtime_privilege_matrix")
	if _, err := verificationPool.Exec(ctx, `GRANT INSERT ON inventory_movements TO pharmacycrm_runtime`); err != nil {
		t.Fatal(err)
	}

	if _, err := verificationPool.Exec(ctx, `ALTER TABLE user_sessions DROP CONSTRAINT chk_session_generation; ALTER TABLE user_sessions ADD CONSTRAINT chk_session_generation CHECK (true)`); err != nil {
		t.Fatal(err)
	}
	assertVerificationFailure(23, "session_security_verification")
	if _, err := verificationPool.Exec(ctx, `ALTER TABLE user_sessions DROP CONSTRAINT chk_session_generation; ALTER TABLE user_sessions ADD CONSTRAINT chk_session_generation CHECK (generation > 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := verificationPool.Exec(ctx, `DROP INDEX uq_user_session_rotated_from`); err != nil {
		t.Fatal(err)
	}
	assertVerificationFailure(23, "session_security_verification")
	if _, err := verificationPool.Exec(ctx, `CREATE UNIQUE INDEX uq_user_session_rotated_from ON user_sessions(rotated_from_session_id) WHERE rotated_from_session_id IS NOT NULL`); err != nil {
		t.Fatal(err)
	}

	corrupted := append([]Migration(nil), loaded...)
	corrupted[0].Checksum = strings.Repeat("0", 64)
	if _, err := Run(ctx, pool, corrupted); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestUpgradeFromSchema19Integration(t *testing.T) {
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
	parsedDSN, err := url.Parse(migrationDSN)
	if err != nil || parsedDSN.Scheme == "" || parsedDSN.Host == "" {
		t.Fatal("POSTGRES_TEST_DSN must be a PostgreSQL URL")
	}
	databaseName := "pharmacycrm_schema19_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	parsedDSN.Path = "/" + databaseName
	isolatedDSN := parsedDSN.String()
	migrationConfig, err := pgxpool.ParseConfig(migrationDSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{databaseName}.Sanitize()+" OWNER "+pgx.Identifier{migrationConfig.ConnConfig.User}.Sanitize()+" TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{databaseName}.Sanitize()+" WITH (FORCE)")
	})
	pool, err := database.NewMigration(ctx, config.MigrationPostgresConfig{DSN: isolatedDSN, PoolConfig: config.PoolConfig{
		MinConnections: 1, MaxConnections: 2, AcquireTimeout: 5 * time.Second,
		MaxConnectionLife: time.Minute, MaxConnectionIdle: time.Minute, HealthCheckPeriod: time.Minute,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	loaded, err := Load(embeddedmigrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, pool, loaded[:19]); err != nil {
		t.Fatal(err)
	}
	result, err := Run(ctx, pool, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 24 || len(result.Applied) != 5 || result.Applied[0] != 20 || result.Applied[4] != 24 {
		t.Fatalf("unexpected schema 19 upgrade result: %#v", result)
	}
	replayed, err := Run(ctx, pool, loaded)
	if err != nil || len(replayed.Applied) != 0 || replayed.SchemaVersion != 24 {
		t.Fatalf("schema 24 replay=%#v err=%v", replayed, err)
	}
}

func TestUpgradeFromSchema21Integration(t *testing.T) {
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
	parsedDSN, err := url.Parse(migrationDSN)
	if err != nil || parsedDSN.Scheme == "" || parsedDSN.Host == "" {
		t.Fatal("POSTGRES_TEST_DSN must be a PostgreSQL URL")
	}
	databaseName := "pharmacycrm_schema21_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	parsedDSN.Path = "/" + databaseName
	isolatedDSN := parsedDSN.String()
	migrationConfig, err := pgxpool.ParseConfig(migrationDSN)
	if err != nil {
		t.Fatal(err)
	}
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+identifier+" OWNER "+pgx.Identifier{migrationConfig.ConnConfig.User}.Sanitize()+" TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	})
	pool, err := database.NewMigration(ctx, config.MigrationPostgresConfig{DSN: isolatedDSN, PoolConfig: config.PoolConfig{
		MinConnections: 1, MaxConnections: 2, AcquireTimeout: 5 * time.Second,
		MaxConnectionLife: time.Minute, MaxConnectionIdle: time.Minute, HealthCheckPeriod: time.Minute,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	loaded, err := Load(embeddedmigrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := Run(ctx, pool, loaded[:21]); err != nil || result.SchemaVersion != 21 {
		t.Fatalf("schema 21 setup=%#v err=%v", result, err)
	}
	result, err := Run(ctx, pool, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 24 || len(result.Applied) != 3 || result.Applied[0] != 22 || result.Applied[1] != 23 || result.Applied[2] != 24 {
		t.Fatalf("unexpected schema 21 upgrade result: %#v", result)
	}
	replayed, err := Run(ctx, pool, loaded)
	if err != nil || len(replayed.Applied) != 0 || replayed.SchemaVersion != 24 {
		t.Fatalf("schema 24 replay=%#v err=%v", replayed, err)
	}
}
