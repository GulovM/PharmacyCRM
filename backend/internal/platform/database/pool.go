// Package database owns PostgreSQL technical primitives.
package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidPostgresConfiguration = errors.New("invalid postgres configuration")

// ConfigurationError intentionally omits the DSN and any parser text from its
// message. Raw parser errors may echo a password-bearing connection string.
type ConfigurationError struct{ Err error }

func (e *ConfigurationError) Error() string        { return ErrInvalidPostgresConfiguration.Error() }
func (e *ConfigurationError) Unwrap() error        { return e.Err }
func (e *ConfigurationError) Is(target error) bool { return target == ErrInvalidPostgresConfiguration }

// Pool wraps pgxpool with the configured acquire timeout. It is a platform
// primitive; application and domain packages must not depend on it.
type Pool struct {
	pool           *pgxpool.Pool
	acquireTimeout time.Duration
}

func NewAPI(ctx context.Context, cfg config.APIPostgresConfig) (*Pool, error) {
	return newPool(ctx, cfg.PoolConfig, cfg.DSN)
}
func NewWorker(ctx context.Context, cfg config.WorkerPostgresConfig) (*Pool, error) {
	return newPool(ctx, cfg.PoolConfig, cfg.DSN)
}
func NewMigration(ctx context.Context, cfg config.MigrationPostgresConfig) (*Pool, error) {
	return newPool(ctx, cfg.PoolConfig, cfg.DSN)
}

func newPool(ctx context.Context, cfg config.PoolConfig, dsn string) (*Pool, error) {
	poolConfig, err := BuildPoolConfig(cfg, dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	return &Pool{pool: pool, acquireTimeout: cfg.AcquireTimeout}, nil
}

// BuildPoolConfig is exposed for deterministic configuration tests and never
// returns a DSN in an error message.
func BuildPoolConfig(cfg config.PoolConfig, dsn string) (*pgxpool.Config, error) {
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, &ConfigurationError{Err: ErrInvalidPostgresConfiguration}
	}
	poolConfig.MinConns = cfg.MinConnections
	poolConfig.MaxConns = cfg.MaxConnections
	poolConfig.MaxConnLifetime = cfg.MaxConnectionLife
	poolConfig.MaxConnIdleTime = cfg.MaxConnectionIdle
	poolConfig.HealthCheckPeriod = cfg.HealthCheckPeriod
	poolConfig.ConnConfig.ConnectTimeout = cfg.AcquireTimeout
	return poolConfig, nil
}

// Acquire propagates cancellation and adds the bounded configured wait time.
func (p *Pool) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, p.acquireTimeout)
	defer cancel()
	return p.pool.Acquire(ctx)
}
func (p *Pool) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p *Pool) SchemaVersion(ctx context.Context) (int64, error) {
	var version int64
	if err := p.pool.QueryRow(ctx, "SELECT schema_version FROM pharmacycrm_schema_metadata WHERE singleton").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}
func (p *Pool) AcquireConn(ctx context.Context) (*pgxpool.Conn, error) { return p.Acquire(ctx) }
func (p *Pool) Close()                                                 { p.pool.Close() }
func (p *Pool) Stat() *pgxpool.Stat                                    { return p.pool.Stat() }
