// Package migration runs embedded forward PostgreSQL migrations.
package migration

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
)

const advisoryLockKey int64 = 706515008

var filename = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.up\.sql$`)

type Migration struct {
	Version             int64
	Name, SQL, Checksum string
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
		return nil, err
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
			return nil, err
		}
		sum := sha256.Sum256(raw)
		result = append(result, Migration{version, matches[2], string(raw), fmt.Sprintf("%x", sum)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}

func Run(ctx context.Context, pool *database.Pool, migrations []Migration) (Result, error) {
	conn, err := pool.AcquireConn(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire migration connection")
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin migration transaction")
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey); err != nil {
		return Result{}, fmt.Errorf("acquire migration lock")
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS pharmacycrm_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return Result{}, fmt.Errorf("initialize migration metadata")
	}
	rows, err := tx.Query(ctx, "SELECT version, checksum FROM pharmacycrm_schema_migrations")
	if err != nil {
		return Result{}, fmt.Errorf("read migration history")
	}
	applied := map[int64]string{}
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return Result{}, fmt.Errorf("scan migration history")
		}
		applied[version] = checksum
	}
	rows.Close()
	result := Result{Status: "ok", Applied: []int64{}, FinishedAt: time.Now().UTC()}
	for _, migration := range migrations {
		if checksum, ok := applied[migration.Version]; ok {
			if checksum != migration.Checksum {
				return Result{}, fmt.Errorf("migration checksum mismatch")
			}
			result.SchemaVersion = migration.Version
			continue
		}
		if _, err := tx.Exec(ctx, migration.SQL); err != nil {
			return Result{}, fmt.Errorf("apply migration %d", migration.Version)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO pharmacycrm_schema_migrations (version,name,checksum) VALUES ($1,$2,$3)", migration.Version, migration.Name, migration.Checksum); err != nil {
			return Result{}, fmt.Errorf("record migration %d", migration.Version)
		}
		result.Applied = append(result.Applied, migration.Version)
		result.SchemaVersion = migration.Version
	}
	var recordedVersion int64
	var recordedCount int
	if err := tx.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0), COUNT(*) FROM pharmacycrm_schema_migrations").Scan(&recordedVersion, &recordedCount); err != nil {
		return Result{}, fmt.Errorf("verify migration metadata")
	}
	if recordedCount != len(migrations) || recordedVersion != result.SchemaVersion {
		return Result{}, fmt.Errorf("verify migration version")
	}
	var declaredVersion int64
	if err := tx.QueryRow(ctx, "SELECT schema_version FROM pharmacycrm_schema_metadata WHERE singleton").Scan(&declaredVersion); err != nil {
		return Result{}, fmt.Errorf("verify schema metadata")
	}
	if declaredVersion != result.SchemaVersion {
		return Result{}, fmt.Errorf("verify declared schema version")
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit migrations")
	}
	return result, nil
}
