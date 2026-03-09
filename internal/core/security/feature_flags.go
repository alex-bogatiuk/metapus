package security

import (
	"context"
	"sync"
)

// FeatureFlagProvider provides feature flag evaluation.
// Abstraction allows different backends: in-memory, Redis, LaunchDarkly, etc.
type FeatureFlagProvider interface {
	// IsEnabled checks if feature is enabled for context
	IsEnabled(ctx context.Context, flag string) bool
	
	// GetVariant returns variant name for A/B tests
	GetVariant(ctx context.Context, flag string) string
	
	// GetValue returns typed value for feature configuration
	GetValue(ctx context.Context, flag string) any
}

// Feature flag names (constants for type safety)
const (
	FlagNewPostingAlgorithm = "new_posting_algorithm"
	FlagAsyncPosting        = "async_posting"
	FlagAdvancedReports     = "advanced_reports"
	FlagBetaUI              = "beta_ui"
)

// InMemoryFlags is a simple in-memory feature flag provider.
// Suitable for MVP and testing.
type InMemoryFlags struct {
	mu       sync.RWMutex
	flags    map[string]bool
	variants map[string]string
	values   map[string]any
}

// NewInMemoryFlags creates an in-memory flag provider.
func NewInMemoryFlags() *InMemoryFlags {
	return &InMemoryFlags{
		flags:    make(map[string]bool),
		variants: make(map[string]string),
		values:   make(map[string]any),
	}
}

func (f *InMemoryFlags) IsEnabled(ctx context.Context, flag string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.flags[flag]
}

func (f *InMemoryFlags) GetVariant(ctx context.Context, flag string) string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.variants[flag]
}

func (f *InMemoryFlags) GetValue(ctx context.Context, flag string) any {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.values[flag]
}

// SetFlag sets a boolean flag (for testing/admin).
func (f *InMemoryFlags) SetFlag(flag string, enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flags[flag] = enabled
}

// SetVariant sets a variant (for A/B tests).
func (f *InMemoryFlags) SetVariant(flag, variant string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.variants[flag] = variant
}

// SetValue sets a configuration value.
func (f *InMemoryFlags) SetValue(flag string, value any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[flag] = value
}
