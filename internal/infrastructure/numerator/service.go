// Package numerator provides the PostgreSQL implementation of auto-numbering.
// This is the infrastructure layer - it implements core/numerator.Generator.
package numerator

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	corenumerator "metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/tenantdb"
)

// Querier interface for database operations.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Service issues numbers from counters in sys_sequences of the tenant
// database. Each number increments its counter row inside the caller's
// transaction, so the number commits or rolls back with the record that holds
// it; a concurrent write of the same series waits for that commit.
type Service struct {
	// querierFn overrides the default querier resolution (for testing only).
	querierFn func(ctx context.Context) Querier
}

// Ensure compile-time interface compliance.
var _ corenumerator.Generator = (*Service)(nil)

// nextDuration is how long taking a number takes, by series. It includes the
// wait for the counter row held by a concurrent uncommitted write, so a hot
// series shows up here first.
var nextDuration, _ = otel.Meter("metapus/numerator").Float64Histogram("numerator.next.duration",
	metric.WithUnit("s"), metric.WithDescription("Time to take the next number of a series, including the wait for its counter"))

// New creates a new numerator service.
// Uses Database-per-Tenant architecture: tenant pool from context.
func New() *Service { return &Service{} }

// getQuerier returns the querier for the current context.
// Uses querierFn override if set (tests), otherwise tenant pool from context.
func (s *Service) getQuerier(ctx context.Context) Querier {
	if s.querierFn != nil {
		return s.querierFn(ctx)
	}
	// Numbering is part of the command that writes the record. Prefer a
	// transaction-aware adapter so the counter increment commits or rolls back
	// with the record instead of escaping through a separate pool connection.
	if txm, err := tenant.GetTxManager(ctx); err == nil {
		if querier, ok := txm.(Querier); ok {
			return querier
		}
	}
	return tenantdb.MustGetPool(ctx)
}

// GetNextNumber takes the next number of the series for the period, e.g.
// INV-2026-00001.
func (s *Service) GetNextNumber(ctx context.Context, cfg corenumerator.Config, period time.Time) (string, error) {
	if s == nil {
		return "", fmt.Errorf("numerator service is not initialized")
	}
	started := time.Now()
	var num int64
	err := s.getQuerier(ctx).QueryRow(ctx, `
		INSERT INTO sys_sequences (key, current_val)
		VALUES ($1, 1)
		ON CONFLICT (key) DO UPDATE SET current_val = sys_sequences.current_val + 1
		RETURNING current_val
	`, cfg.SequenceKey(period)).Scan(&num)
	nextDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attribute.String("series", seriesName(cfg))))
	if err != nil {
		return "", fmt.Errorf("next number: %w", err)
	}
	return cfg.Format(period, num), nil
}

// seriesName labels a series without its period, so the metric keeps one
// stream per series.
func seriesName(cfg corenumerator.Config) string {
	if cfg.Key != "" {
		return cfg.Key
	}
	return cfg.Prefix
}

// SetNextNumber sets the counter of the series for the period (for
// migration purposes).
func (s *Service) SetNextNumber(ctx context.Context, cfg corenumerator.Config, period time.Time, value int64) error {
	var result int64
	return s.getQuerier(ctx).QueryRow(ctx, `
		INSERT INTO sys_sequences (key, current_val)
		VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET current_val = $2
		RETURNING current_val
	`, cfg.SequenceKey(period), value).Scan(&result)
}

// ParseNumber extracts numeric part from formatted number.
// Returns -1 if parsing fails.
func ParseNumber(formatted string) int64 {
	var num int64
	patterns := []string{
		"%*[^-]-%*d-%d",
		"%*[^-]-%d",
	}

	for _, pattern := range patterns {
		if _, err := fmt.Sscanf(formatted, pattern, &num); err == nil {
			return num
		}
	}

	return -1
}
