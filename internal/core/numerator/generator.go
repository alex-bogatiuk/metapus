// Package numerator provides domain contracts for document auto-numbering.
// Implementations live in infrastructure layer.
package numerator

import (
	"context"
	"time"
)

// Generator generates sequential document numbers.
// This is the domain contract - implementations live in infrastructure layer.
//
// In Database-per-Tenant architecture, implementations should obtain
// database connections from context using tenant.GetPool or tenant.GetTxManager.
type Generator interface {
	// GetNextNumber generates the next document number.
	// Pattern: PREFIX-YEAR-XXXXX (e.g., INV-2024-00001)
	//
	// Supports Strict (DB-level) and Cached (Memory-level) strategies.
	GetNextNumber(ctx context.Context, cfg Config, opts *Options, period time.Time) (string, error)

	// SetNextNumber sets the next number value (for migration purposes).
	SetNextNumber(ctx context.Context, cfg Config, period time.Time, value int64) error
}
