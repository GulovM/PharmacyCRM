// Package database owns PostgreSQL technical primitives.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool with the configured acquire timeout. It is a platform
// primitive; application and domain packages must not depend on it.
type Pool struct {
	pool           *pgxpool.Pool
	acquireTimeout time.Duration
}

func NewRuntime(ctx context.Context, cfg config.PostgresConfig) (*Pool, error) {
	return newPool(ctx, cfg, cfg.RuntimeDSN)
}
func NewMigration(ctx context.Context, cfg config.PostgresConfig) (*Pool, error) {
	return newPool(ctx, cfg, cfg.MigrationDSN)
}

func newPool(ctx context.Context, cfg config.PostgresConfig, dsn string) (*Pool, error) {
	poolConfig, err := BuildPoolConfig(cfg, dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool")
	}
	return &Pool{pool: pool, acquireTimeout: cfg.AcquireTimeout}, nil
}

// BuildPoolConfig is exposed for deterministic configuration tests and never
// returns a DSN in an error message.
func BuildPoolConfig(cfg config.PostgresConfig, dsn string) (*pgxpool.Config, error) {
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres pool configuration")
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
func (p *Pool) Ping(ctx context.Context) error                         { return p.pool.Ping(ctx) }
func (p *Pool) AcquireConn(ctx context.Context) (*pgxpool.Conn, error) { return p.Acquire(ctx) }
func (p *Pool) Close()                                                 { p.pool.Close() }
func (p *Pool) Stat() *pgxpool.Stat                                    { return p.pool.Stat() }
