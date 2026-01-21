// Package postgres provides PostgreSQL infrastructure components.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/pkg/logger"
)

// PoolConfig holds connection pool configuration.
type PoolConfig struct {
	DSN               string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
}

// DefaultPoolConfig returns sensible defaults for production.
func DefaultPoolConfig(dsn string) PoolConfig {
	return PoolConfig{
		DSN:               dsn,
		MaxConns:          25,
		MinConns:          5,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
	}
}

// Pool wraps pgxpool.Pool to provide a clean interface.
type Pool struct {
	*pgxpool.Pool
}

// Close closes all connections in the pool.
func (p *Pool) Close() {
	if p.Pool != nil {
		p.Pool.Close()
	}
}

// Unwrap returns the underlying pgxpool.Pool for cases where it's needed.
func (p *Pool) Unwrap() *pgxpool.Pool {
	return p.Pool
}

// NewPool creates a new connection pool with the given configuration.
func NewPool(ctx context.Context, cfg PoolConfig) (*Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}

	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = cfg.HealthCheckPeriod

	// Custom connection setup
	poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		// Set application name for debugging
		_, err := conn.Exec(ctx, "SET application_name = 'metapus'")
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Pool{Pool: pool}, nil
}

// PoolStats returns current pool statistics for metrics.
type PoolStats struct {
	TotalConns     int32
	AcquiredConns  int32
	IdleConns      int32
	MaxConns       int32
	AcquireCount   int64
	AcquireDuration time.Duration
}

// GetPoolStats extracts statistics from pool.
func GetPoolStats(pool *pgxpool.Pool) PoolStats {
	stat := pool.Stat()
	return PoolStats{
		TotalConns:      stat.TotalConns(),
		AcquiredConns:   stat.AcquiredConns(),
		IdleConns:       stat.IdleConns(),
		MaxConns:        stat.MaxConns(),
		AcquireCount:    stat.AcquireCount(),
		AcquireDuration: stat.AcquireDuration(),
	}
}

// LogPoolStats logs pool statistics.
func LogPoolStats(ctx context.Context, pool *pgxpool.Pool) {
	stats := GetPoolStats(pool)
	logger.Info(ctx, "database pool stats",
		"total", stats.TotalConns,
		"acquired", stats.AcquiredConns,
		"idle", stats.IdleConns,
		"max", stats.MaxConns,
	)
}
