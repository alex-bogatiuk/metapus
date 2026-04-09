// Package numerator provides PostgreSQL implementation of document auto-numbering.
// This is the infrastructure layer - it implements core/numerator.Generator interface.
package numerator

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	corenumerator "metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
)

// Querier interface for database operations.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type cachedRange struct {
	current int64
	max     int64
}

// numShards is the number of independent lock shards.
// 16 is a good trade-off between contention reduction and memory overhead.
const numShards = 16

// shard holds a mutex and a ranges map for a subset of cache keys.
type shard struct {
	mu     sync.Mutex
	ranges map[string]*cachedRange
}

// Service provides document numbering functionality using PostgreSQL.
// Uses Database-per-Tenant architecture: querier is obtained from context.
// Cache keys are sharded across 16 independent mutexes to reduce lock contention
// when multiple tenants generate numbers concurrently.
type Service struct {
	shards [numShards]shard
	// querierFn overrides the default querier resolution (for testing only).
	querierFn func(ctx context.Context) Querier
}

// Ensure compile-time interface compliance.
var _ corenumerator.Generator = (*Service)(nil)

// New creates a new numerator service.
// Uses Database-per-Tenant architecture: tenant pool from context.
func New() *Service {
	s := &Service{}
	for i := range s.shards {
		s.shards[i].ranges = make(map[string]*cachedRange)
	}
	return s
}

// getShard returns the shard responsible for the given cache key.
func (s *Service) getShard(key string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &s.shards[h.Sum32()%numShards]
}

// getQuerier returns the querier for the current context.
// Uses querierFn override if set (tests), otherwise tenant pool from context.
func (s *Service) getQuerier(ctx context.Context) Querier {
	if s.querierFn != nil {
		return s.querierFn(ctx)
	}
	return tenant.MustGetPool(ctx)
}

// GetNextNumber generates the next document number.
// Pattern: PREFIX-YEAR-XXXXX (e.g., INV-2024-00001)
//
// Supports Strict (DB-level) and Cached (Memory-level) strategies.
func (s *Service) GetNextNumber(ctx context.Context, cfg corenumerator.Config, opts *corenumerator.Options, period time.Time) (string, error) {
	if s == nil {
		return "", fmt.Errorf("numerator service is not initialized")
	}

	if opts == nil {
		opts = corenumerator.DefaultOptions()
	}

	key := s.buildKey(cfg, period)
	var num int64
	var err error

	// Prepend tenant ID to the cache key to avoid cross-tenant collisions
	cacheKey := key
	if tenantID := tenant.GetTenantID(ctx); tenantID != "" {
		cacheKey = fmt.Sprintf("%s:%s", tenantID, key)
	}

	switch opts.Strategy {
	case corenumerator.StrategyCached:
		num, err = s.getNextCached(ctx, key, cacheKey, opts)
	case corenumerator.StrategyStrict:
		fallthrough
	default:
		num, err = s.getNextStrict(ctx, key)
	}

	if err != nil {
		return "", err
	}

	return s.formatNumber(cfg, period, num), nil
}

// getNextStrict fetches the next number directly from DB using UPSERT + RETURNING.
func (s *Service) getNextStrict(ctx context.Context, key string) (int64, error) {
	querier := s.getQuerier(ctx)
	var num int64

	err := querier.QueryRow(ctx, `
		INSERT INTO sys_sequences (key, current_val)
		VALUES ($1, 1)
		ON CONFLICT (key) DO UPDATE SET current_val = sys_sequences.current_val + 1
		RETURNING current_val
	`, key).Scan(&num)
	if err != nil {
		return 0, fmt.Errorf("strict next: %w", err)
	}

	return num, nil
}


// getNextCached fetches next number from memory, refilling from DB if needed.
// Uses per-shard locking to reduce contention across tenants.
func (s *Service) getNextCached(ctx context.Context, dbKey, cacheKey string, opts *corenumerator.Options) (int64, error) {
	sh := s.getShard(cacheKey)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	rng, exists := sh.ranges[cacheKey]
	if !exists {
		rng = &cachedRange{}
		sh.ranges[cacheKey] = rng
	}

	// allocate new range if needed
	if rng.current >= rng.max {
		size := opts.RangeSize
		if size <= 0 {
			size = 50 // default
		}

		querier := s.getQuerier(ctx)
		var newMax int64

		increment := size

		err := querier.QueryRow(ctx, `
            INSERT INTO sys_sequences (key, current_val)
            VALUES ($1, $2)
            ON CONFLICT (key) DO UPDATE SET current_val = sys_sequences.current_val + $2
            RETURNING current_val
		`, dbKey, increment).Scan(&newMax)

		if err != nil {
			return 0, fmt.Errorf("reserve range: %w", err)
		}

		// newMax is the end of our range.
		// The start of our range is newMax - increment + 1.
		// If row absent: INSERT ... VALUES (key, increment). current_val = increment.
		// Range is 1..increment.
		// If row present: current_val += increment.
		// Range is (old_max + 1) .. new_max.

		rng.current = newMax - increment // Set current to one BEFORE the first valid number
		rng.max = newMax
	}

	rng.current++
	return rng.current, nil
}

// SetNextNumber sets the next number value (for migration purposes).
func (s *Service) SetNextNumber(ctx context.Context, cfg corenumerator.Config, period time.Time, value int64) error {
	key := s.buildKey(cfg, period)
	querier := s.getQuerier(ctx)

	var result int64
	err := querier.QueryRow(ctx, `
		INSERT INTO sys_sequences (key, current_val)
		VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET current_val = $2
		RETURNING current_val
	`, key, value).Scan(&result)

	// Invalidate cache for this key if exists
	cacheKey := key
	if tid := tenant.GetTenantID(ctx); tid != "" {
		cacheKey = fmt.Sprintf("%s:%s", tid, key)
	}
	sh := s.getShard(cacheKey)
	sh.mu.Lock()
	delete(sh.ranges, cacheKey)
	sh.mu.Unlock()

	return err
}

// buildKey creates the sequence key based on config and period.
func (s *Service) buildKey(cfg corenumerator.Config, period time.Time) string {
	switch cfg.ResetPeriod {
	case "month":
		return fmt.Sprintf("%s_%s", cfg.Prefix, period.Format("2006_01"))
	case "year":
		return fmt.Sprintf("%s_%s", cfg.Prefix, period.Format("2006"))
	default:
		return cfg.Prefix
	}
}

// formatNumber creates the final number string.
func (s *Service) formatNumber(cfg corenumerator.Config, period time.Time, num int64) string {
	padWidth := cfg.PadWidth
	if padWidth == 0 {
		padWidth = 5
	}

	if cfg.IncludeYear {
		return fmt.Sprintf("%s-%s-%0*d", cfg.Prefix, period.Format("2006"), padWidth, num)
	}
	return fmt.Sprintf("%s-%0*d", cfg.Prefix, padWidth, num)
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
