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
// database connections from context using tenant.GetTxManager.
type Generator interface {
	// GetNextNumber takes the next number of the series for the period, e.g.
	// INV-2026-00001, in the transaction of ctx: the number commits or rolls
	// back with the record that holds it.
	GetNextNumber(ctx context.Context, cfg Config, period time.Time) (string, error)

	// SetNextNumber sets the next number value (for migration purposes).
	SetNextNumber(ctx context.Context, cfg Config, period time.Time, value int64) error
}
