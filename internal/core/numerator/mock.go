// Package numerator provides domain contracts for document auto-numbering.
package numerator

import (
	"context"
	"time"
)

// MockGenerator is a test implementation of Generator.
// Use in unit tests to avoid database dependencies.
type MockGenerator struct {
	GetNextNumberFunc func(ctx context.Context, cfg Config, opts *Options, period time.Time) (string, error)
	SetNextNumberFunc func(ctx context.Context, cfg Config, period time.Time, value int64) error
}

// GetNextNumber implements Generator.
func (m *MockGenerator) GetNextNumber(ctx context.Context, cfg Config, opts *Options, period time.Time) (string, error) {
	if m.GetNextNumberFunc != nil {
		return m.GetNextNumberFunc(ctx, cfg, opts, period)
	}
	// Default: return predictable mock number
	return "MOCK-2026-00001", nil
}

// SetNextNumber implements Generator.
func (m *MockGenerator) SetNextNumber(ctx context.Context, cfg Config, period time.Time, value int64) error {
	if m.SetNextNumberFunc != nil {
		return m.SetNextNumberFunc(ctx, cfg, period, value)
	}
	return nil
}

// Ensure compile-time interface compliance.
var _ Generator = (*MockGenerator)(nil)
