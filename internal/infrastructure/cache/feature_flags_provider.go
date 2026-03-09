// Package cache provides caching infrastructure.
package cache

import (
	"context"

	"metapus/internal/core/security"
)

// CacheBackedFlags implements security.FeatureFlagProvider using SchemaCache.
// This provides type-safe, context-aware feature flag access with automatic
// invalidation via PostgreSQL NOTIFY.
type CacheBackedFlags struct {
	cache *SchemaCache
}

// NewCacheBackedFlags creates a feature flag provider backed by schema cache.
func NewCacheBackedFlags(cache *SchemaCache) *CacheBackedFlags {
	return &CacheBackedFlags{cache: cache}
}

// IsEnabled checks if feature is enabled for context's tenant.
func (f *CacheBackedFlags) IsEnabled(ctx context.Context, flag string) bool {
	return f.cache.IsFeatureEnabled(flag)
}

// GetVariant returns variant name for A/B tests.
func (f *CacheBackedFlags) GetVariant(ctx context.Context, flag string) string {
	return f.cache.GetFeatureVariant(flag)
}

// GetValue returns typed value for feature configuration.
func (f *CacheBackedFlags) GetValue(ctx context.Context, flag string) any {
	// Return a copy of config to avoid external mutation of cache state.
	return f.cache.GetFeatureConfig(flag)
}

// Ensure interface compliance at compile time.
var _ security.FeatureFlagProvider = (*CacheBackedFlags)(nil)
