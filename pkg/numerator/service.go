// Package numerator provides document auto-numbering service.
// In Database-per-Tenant architecture, uses TxManager from context.
package numerator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/tenant"
)

// Strategy defines the numbering generation strategy.
type Strategy int

const (
	// StrategyStrict uses UPDATE ... RETURNING for every number.
	// Guarantees sequential numbers without gaps.
	// Slower, suitable for invoices and accounting documents.
	StrategyStrict Strategy = iota

	// StrategyCached allocates ranges of numbers in memory.
	// Much faster, but may produce gaps if application restarts.
	// Suitable for internal documents (orders, shipments).
	StrategyCached
)

// Options configuration for number generation.
type Options struct {
	// Strategy to use for number generation
	Strategy Strategy
	// RangeSize is the number of IDs to allocate at once in Cached strategy.
	// Default is 50.
	RangeSize int64
}

// DefaultOptions returns standard options (Strict).
func DefaultOptions() *Options {
	return &Options{
		Strategy: StrategyStrict,
	}
}

// Querier interface for database operations.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type cachedRange struct {
	current int64
	max     int64
}

// Service provides document numbering functionality.
// In Database-per-Tenant mode, querier is obtained from context.
type Service struct {
	// staticQuerier is used for single-tenant mode (backwards compatibility)
	staticQuerier Querier
	// useContext indicates whether to get querier from context
	useContext bool

	// cache protects ranges map
	cacheMu sync.Mutex
	// ranges stores active ranges for each key (tenant-aware keys needed?
	// key should include tenant if we cache globally, but Service is usually per-request or global singleton.
	// If Service is singleton, we must ensure keys are unique per tenant or clearing cache.
	// However, current design seems to create Service per usage or just use static/context querier.
	// The cache needs to be safe.
	// WAIT: If NewFromContext usage implies a Singleton service for likely the whole app lifetime?
	// If this service is a singleton, we need to be careful with keys.
	// The current 'key' logic builds string like "INV_2024". Even in multi-tenant,
	// if the Service object is shared, we might mix up tenants if key doesn't include tenant.
	// BUT: In DB-per-tenant, usually code runs in isolated container OR context has connection.
	// Let's assume typical Go app structure: Service is often a singleton.
	// We should probably include tenantID in the cache key if useContext is true,
	// but `GetNextNumber` doesn't strictly take tenantID (it takes context).
	// We can extract tenantID from context if needed, or rely on the fact that
	// `sys_sequences` updates are transaction-bound.
	//
	// FOR MEMORY CACHE: We absolutely need to segregate by Tenant if this service instance is shared.
	// `tenant.MustGetTxManager(ctx)` suggests we have tenant info.
	// Let's assume for now we use a simple map, but effectively
	// we need to be careful. Ideally numerator service should be scoped or key should include tenant.
	// Let's append tenant ID to map key if available in context, or just document this limitation.
	// For "Strict" it doesn't matter (DB enforces it).
	// For "Cached", in-memory state is critical.
	ranges map[string]*cachedRange
}

// New creates a new numerator service with static querier.
// Use for single-tenant or testing scenarios.
func New(querier Querier) *Service {
	return &Service{
		staticQuerier: querier,
		useContext:    false,
		ranges:        make(map[string]*cachedRange),
	}
}

// NewFromContext creates a numerator service that gets querier from context.
// Use for Database-per-Tenant architecture.
func NewFromContext() *Service {
	return &Service{
		useContext: true,
		ranges:     make(map[string]*cachedRange),
	}
}

// getQuerier returns appropriate querier based on configuration.
func (s *Service) getQuerier(ctx context.Context) Querier {
	if s.useContext {
		// Numerator calls are intentionally executed outside of business transactions
		// in this codebase (see domain services: hooks run before tx).
		// So we can safely use tenant pool directly here.
		return tenant.MustGetPool(ctx)
	}
	return s.staticQuerier
}

// Config holds numbering configuration.
type Config struct {
	// Prefix added to all numbers (e.g., "INV", "GR")
	Prefix string

	// IncludeYear adds year to the number
	IncludeYear bool

	// PadWidth is the minimum number width (default 5)
	PadWidth int

	// ResetPeriod: "year", "month", "never"
	ResetPeriod string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig(prefix string) Config {
	return Config{
		Prefix:      prefix,
		IncludeYear: true,
		PadWidth:    5,
		ResetPeriod: "year",
	}
}

// GetNextNumber generates the next document number.
// Pattern: PREFIX-YEAR-XXXXX (e.g., INV-2024-00001)
//
// Supports Strict (DB-level) and Cached (Memory-level) strategies.
func (s *Service) GetNextNumber(ctx context.Context, cfg Config, opts *Options, period time.Time) (string, error) {
	if s == nil {
		return "", fmt.Errorf("numerator service is not initialized")
	}

	if opts == nil {
		opts = DefaultOptions()
	}

	key := s.buildKey(cfg, period)
	var num int64
	var err error

	// If using context (multi-tenant), we MUST prepend tenant ID to the cache key
	// to avoid collisions in the in-memory map if the Service instance is shared.
	cacheKey := key
	if s.useContext {
		if tenantID := tenant.GetTenantID(ctx); tenantID != "" {
			cacheKey = fmt.Sprintf("%s:%s", tenantID, key)
		}
	}

	switch opts.Strategy {
	case StrategyCached:
		num, err = s.getNextCached(ctx, key, cacheKey, opts)
	case StrategyStrict:
		fallthrough
	default:
		num, err = s.getNextStrict(ctx, cfg.Prefix, key, period)
	}

	if err != nil {
		return "", err
	}

	return s.formatNumber(cfg, period, num), nil
}

// getNextStrict fetches the next number directly from DB using UPSERT + RETURNING.
func (s *Service) getNextStrict(ctx context.Context, prefix, key string, period time.Time) (int64, error) {
	querier := s.getQuerier(ctx)
	var num int64

	// Try standard schema (sequence_type + year)
	err := querier.QueryRow(ctx, `
        INSERT INTO sys_sequences (sequence_type, year, current_val)
        VALUES ($1, $2, 1)
        ON CONFLICT (sequence_type, year) DO UPDATE SET current_val = sys_sequences.current_val + 1
        RETURNING current_val
	`, prefix, period.Year()).Scan(&num)

	if err != nil {
		// Try alternative schema (key-based)
		err = querier.QueryRow(ctx, `
            INSERT INTO sys_sequences (key, current_val)
            VALUES ($1, 1)
            ON CONFLICT (key) DO UPDATE SET current_val = sys_sequences.current_val + 1
            RETURNING current_val
		`, key).Scan(&num)

		if err != nil {
			return 0, fmt.Errorf("strict next: %w", err)
		}
	}
	return num, nil
}

// getNextCached fetches next number from memory, refilling from DB if needed.
func (s *Service) getNextCached(ctx context.Context, dbKey, cacheKey string, opts *Options) (int64, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	rng, exists := s.ranges[cacheKey]
	if !exists {
		rng = &cachedRange{}
		s.ranges[cacheKey] = rng
	}

	// allocate new range if needed
	if rng.current >= rng.max {
		size := opts.RangeSize
		if size <= 0 {
			size = 50 // default
		}

		querier := s.getQuerier(ctx)
		var newMax int64

		// We need to reserve 'size' numbers.
		// DB 'current_val' tracks the LAST allocated number.
		// So we want to bump it by 'size'.
		// The range we get is (old_val + 1) to (old_val + size).
		// Wait. `current_val` in `sys_sequences` usually means "last used value" or "next value"?
		// Looking at Strict implementation:
		// VALUES ($1, $2, 1) ... ON CONFLICT ... SET current_val = sys_sequences.current_val + 1 RETURNING current_val
		// If initial insert: returns 1.
		// If update: current_val becomes +1 and returns it.
		// So `current_val` is the VALUE RETURNED to the user.
		//
		// To reserve 50:
		// We want to return X+50.
		// The available range will be [X+1, X+50].
		// X is the value BEFORE update.

		// However, we need to handle the INSERT vs UPDATE case carefully.
		// Simplest way: always use key-based UPSERT for cached ranges to be generic?
		// Or try strict schema first? Let's use key-based as it seems more robust for generic keys.
		// Actually strict uses `prefix` for checking.
		// Let's stick to the key-based approach for simplicity in cached mode logic,
		// OR replicate the fallback logic.

		// Let's assume generic key approach for cached ranges to avoid complex fallbacks in loop.
		// Note: The key passed is constructed from buildKey.
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
		// If we just inserted 1 (wait, if we insert, we insert 'increment'? No, if we insert, we start at ...?)
		// If row absent: INSERT ... VALUES (key, increment). current_val = increment.
		// Range is 1..increment.
		// If row present: current_val += increment.
		// Range is (old_max + 1) .. new_max.
		// Correct.

		rng.current = newMax - increment // Set current to one BEFORE the first valid number
		rng.max = newMax
	}

	rng.current++
	return rng.current, nil
}

// SetNextNumber sets the next number value (for migration purposes).
func (s *Service) SetNextNumber(ctx context.Context, cfg Config, period time.Time, value int64) error {
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
	s.cacheMu.Lock()
	// We might need to iterate or construct cacheKey.
	// Ideally we clear all related keys or just the one if we can reconstruct it.
	// For now, let's just clear for safety if keys match, but we don't know tenant here easily without context?
	// Actually we do have context.
	cacheKey := key
	if s.useContext {
		if tid := tenant.GetTenantID(ctx); tid != "" {
			cacheKey = fmt.Sprintf("%s:%s", tid, key)
		}
	}
	delete(s.ranges, cacheKey)
	s.cacheMu.Unlock()

	return err
}

// buildKey creates the sequence key based on config and period.
func (s *Service) buildKey(cfg Config, period time.Time) string {
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
func (s *Service) formatNumber(cfg Config, period time.Time, num int64) string {
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

// Next generates the next number using default config with prefix.
func (s *Service) Next(ctx context.Context, prefix string, orgID any) (string, error) {
	cfg := DefaultConfig(prefix)
	return s.GetNextNumber(ctx, cfg, nil, time.Now())
}
