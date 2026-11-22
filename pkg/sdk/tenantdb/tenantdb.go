// Package tenantdb resolves a tenant's database pool, for routes that
// find their tenant themselves (a merchant API key, a checkout link).
package tenantdb

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	core "metapus/internal/infrastructure/tenantdb"
)

type (
	// ManagedPool wraps pgxpool.Pool with lifecycle tracking.
	ManagedPool = core.ManagedPool
	// Manager manages database connections for multiple tenants.
	// Thread-safe for concurrent access.
	Manager = core.Manager
)

// MustGetPool retrieves database pool or panics.
// Use in places where missing pool is a programming error.
func MustGetPool(ctx context.Context) *pgxpool.Pool { return core.MustGetPool(ctx) }

// WithPool stores database pool in context.
func WithPool(ctx context.Context, pool *pgxpool.Pool) context.Context {
	return core.WithPool(ctx, pool)
}
