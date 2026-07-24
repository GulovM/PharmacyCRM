// Package migration runs embedded forward PostgreSQL migrations.
package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
)

const advisoryLockKey int64 = 706515008

var filename = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.up\.sql$`)
var verificationQuery = regexp.MustCompile(`(?m)^-- .*Verification query:\s*(SELECT .+;)\s*$`)
var supersedesVerificationLine = regexp.MustCompile(`(?m)^-- Supersedes verification:\s*(.*?)\s*$`)

var (
	ErrChecksumMismatch   = errors.New("migration checksum mismatch")
	ErrVerificationFailed = errors.New("migration verification failed")
)

// VerificationError identifies the migration whose post-apply verification did
// not succeed while retaining the original database or sentinel error.
type VerificationError struct {
	Version int64
	Name    string
	Err     error
}

func (e *VerificationError) Error() string {
	return fmt.Sprintf("verify migration %d %s: %v", e.Version, e.Name, e.Err)
}

func (e *VerificationError) Unwrap() error { return e.Err }

const legacySchemaMetadataVerification = "SELECT to_regclass('public.pharmacycrm_schema_metadata') IS NOT NULL;"

type Migration struct {
	Version                              int64
	Name, SQL, Checksum, VerificationSQL string
}
type Result struct {
	Status        string    `json:"status"`
	Applied       []int64   `json:"applied"`
	SchemaVersion int64     `json:"schema_version"`
	FinishedAt    time.Time `json:"finished_at"`
}

func Load(files fs.FS) ([]Migration, error) {
	names, err := fs.Glob(files, "*.up.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	result := make([]Migration, 0, len(names))
	seen := map[int64]bool{}
	for _, name := range names {
		matches := filename.FindStringSubmatch(name)
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename")
		}
		version, _ := strconv.ParseInt(matches[1], 10, 64)
		if seen[version] {
			return nil, fmt.Errorf("duplicate migration version")
		}
		seen[version] = true
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := parseSupersededVerifications(string(raw), version); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		verification := verificationQuery.FindStringSubmatch(string(raw))
		verificationSQL := ""
		if verification == nil && version == 1 && matches[2] == "schema_metadata" {
			// Migration 000001 predates executable verification queries and is
			// immutable because its checksum is already persisted by E1 databases.
			verificationSQL = legacySchemaMetadataVerification
		} else if verification == nil {
			return nil, fmt.Errorf("migration %d has no verification query", version)
		} else {
			verificationSQL = verification[1]
		}
		result = append(result, Migration{version, matches[2], string(raw), fmt.Sprintf("%x", sum), verificationSQL})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	if _, err := supersededVerificationSet(result); err != nil {
		return nil, err
	}
	return result, nil
}

func Run(ctx context.Context, pool *database.Pool, migrations []Migration) (Result, error) {
	superseded, err := supersededVerificationSet(migrations)
	if err != nil {
		return Result{}, err
	}
	conn, err := pool.AcquireConn(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey); err != nil {
		return Result{}, fmt.Errorf("acquire migration lock: %w", err)
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS pharmacycrm_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return Result{}, fmt.Errorf("initialize migration metadata: %w", err)
	}
	rows, err := tx.Query(ctx, "SELECT version, checksum FROM pharmacycrm_schema_migrations")
	if err != nil {
		return Result{}, fmt.Errorf("read migration history: %w", err)
	}
	applied := map[int64]string{}
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return Result{}, fmt.Errorf("scan migration history: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, fmt.Errorf("iterate migration history: %w", err)
	}
	rows.Close()

	result := Result{Status: "ok", Applied: []int64{}, FinishedAt: time.Now().UTC()}
	for _, migration := range migrations {
		if checksum, ok := applied[migration.Version]; ok {
			if checksum != migration.Checksum {
				return Result{}, fmt.Errorf("migration %d: %w", migration.Version, ErrChecksumMismatch)
			}
			result.SchemaVersion = migration.Version
			continue
		}
		if _, err := tx.Exec(ctx, migration.SQL); err != nil {
			return Result{}, fmt.Errorf("apply migration %d %s: %w", migration.Version, migration.Name, err)
		}
		if _, err := tx.Exec(ctx, "UPDATE pharmacycrm_schema_metadata SET schema_version = $1, updated_at = now() WHERE singleton", migration.Version); err != nil {
			return Result{}, fmt.Errorf("update declared schema version for migration %d: %w", migration.Version, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO pharmacycrm_schema_migrations (version,name,checksum) VALUES ($1,$2,$3)", migration.Version, migration.Name, migration.Checksum); err != nil {
			return Result{}, fmt.Errorf("record migration %d %s: %w", migration.Version, migration.Name, err)
		}
		// Verify the migration before any later forward migration can legally
		// supersede part of its postcondition.
		if err := verifyMigration(ctx, tx, migration); err != nil {
			return Result{}, err
		}
		result.Applied = append(result.Applied, migration.Version)
		result.SchemaVersion = migration.Version
	}

	// A no-op deployment and the final state of an upgrade validate every
	// non-superseded postcondition. Later migrations must explicitly declare
	// historical verifications they replace; checksums of old migrations remain
	// immutable and all unrelated drift detection stays active.
	for _, migration := range migrations {
		if _, skip := superseded[migration.Version]; skip {
			continue
		}
		if err := verifyMigration(ctx, tx, migration); err != nil {
			return Result{}, err
		}
	}

	var recordedVersion int64
	var recordedCount int
	if err := tx.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0), COUNT(*) FROM pharmacycrm_schema_migrations").Scan(&recordedVersion, &recordedCount); err != nil {
		return Result{}, fmt.Errorf("verify migration metadata: %w", err)
	}
	if recordedCount != len(migrations) || recordedVersion != result.SchemaVersion {
		return Result{}, fmt.Errorf("verify migration version")
	}
	var declaredVersion int64
	if err := tx.QueryRow(ctx, "SELECT schema_version FROM pharmacycrm_schema_metadata WHERE singleton").Scan(&declaredVersion); err != nil {
		return Result{}, fmt.Errorf("verify schema metadata: %w", err)
	}
	if declaredVersion != result.SchemaVersion {
		return Result{}, fmt.Errorf("verify declared schema version")
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit migrations: %w", err)
	}
	return result, nil
}

func verifyMigration(ctx context.Context, tx pgx.Tx, migration Migration) error {
	var verified bool
	if err := tx.QueryRow(ctx, migration.VerificationSQL).Scan(&verified); err != nil {
		return &VerificationError{Version: migration.Version, Name: migration.Name, Err: err}
	}
	if !verified {
		return &VerificationError{Version: migration.Version, Name: migration.Name, Err: ErrVerificationFailed}
	}
	return nil
}

func supersededVerificationSet(migrations []Migration) (map[int64]struct{}, error) {
	known := make(map[int64]struct{}, len(migrations))
	for _, migration := range migrations {
		known[migration.Version] = struct{}{}
	}
	result := make(map[int64]struct{})
	for _, migration := range migrations {
		versions, err := parseSupersededVerifications(migration.SQL, migration.Version)
		if err != nil {
			return nil, err
		}
		for _, version := range versions {
			if _, exists := known[version]; !exists {
				return nil, fmt.Errorf("migration %d supersedes unknown verification %d", migration.Version, version)
			}
			result[version] = struct{}{}
		}
	}
	return result, nil
}

func parseSupersededVerifications(sql string, migrationVersion int64) ([]int64, error) {
	matches := supersedesVerificationLine.FindAllStringSubmatch(sql, -1)
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("migration %d has multiple superseded verification declarations", migrationVersion)
	}
	declaration := strings.TrimSpace(matches[0][1])
	if declaration == "" {
		return nil, fmt.Errorf("migration %d has an empty superseded verification declaration", migrationVersion)
	}
	seen := map[int64]struct{}{}
	versions := make([]int64, 0)
	for _, part := range strings.Split(declaration, ",") {
		value := strings.TrimSpace(part)
		version, err := strconv.ParseInt(value, 10, 64)
		if err != nil || version < 1 || version >= migrationVersion {
			return nil, fmt.Errorf("migration %d has invalid superseded verification %q", migrationVersion, value)
		}
		if _, duplicate := seen[version]; duplicate {
			return nil, fmt.Errorf("migration %d repeats superseded verification %d", migrationVersion, version)
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}
	return versions, nil
}
