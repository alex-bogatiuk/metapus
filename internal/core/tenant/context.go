package tenant

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/tx"
)

// Context keys for tenant-related values.
type ctxKey int

const (
	poolKey ctxKey = iota
	txManagerKey
	tenantKey
)

// Errors for context operations.
var (
	ErrNoTenantInContext = errors.New("tenant not found in context")
	ErrNoPoolInContext   = errors.New("database pool not found in context")
	ErrNoTxManager       = errors.New("transaction manager not found in context")
)

// --- Pool ---

// WithPool stores database pool in context.
func WithPool(ctx context.Context, pool *pgxpool.Pool) context.Context {
	return context.WithValue(ctx, poolKey, pool)
}

// GetPool retrieves database pool from context.
func GetPool(ctx context.Context) (*pgxpool.Pool, error) {
	pool, ok := ctx.Value(poolKey).(*pgxpool.Pool)
	if !ok || pool == nil {
		return nil, ErrNoPoolInContext
	}
	return pool, nil
}

// MustGetPool retrieves database pool or panics.
// Use in places where missing pool is a programming error.
func MustGetPool(ctx context.Context) *pgxpool.Pool {
	pool, err := GetPool(ctx)
	if err != nil {
		panic("database pool not in context: " + err.Error())
	}
	return pool
}

// --- TxManager ---

// WithTxManager stores TxManager in context.
func WithTxManager(ctx context.Context, txm tx.Manager) context.Context {
	return context.WithValue(ctx, txManagerKey, txm)
}

// GetTxManager retrieves TxManager from context.
func GetTxManager(ctx context.Context) (tx.Manager, error) {
	txm, ok := ctx.Value(txManagerKey).(tx.Manager)
	if !ok || txm == nil {
		return nil, ErrNoTxManager
	}
	return txm, nil
}

// MustGetTxManager retrieves TxManager or panics.
// Use in places where missing TxManager is a programming error.
func MustGetTxManager(ctx context.Context) tx.Manager {
	txm, err := GetTxManager(ctx)
	if err != nil {
		panic("TxManager not in context: " + err.Error())
	}
	return txm
}

// --- Tenant ---

// WithTenant stores tenant info in context.
func WithTenant(ctx context.Context, t *Tenant) context.Context {
	return context.WithValue(ctx, tenantKey, t)
}

// GetTenant retrieves tenant from context.
func GetTenant(ctx context.Context) *Tenant {
	t, _ := ctx.Value(tenantKey).(*Tenant)
	return t
}

// GetTenantID returns tenant ID or empty string.
// Alias for backwards compatibility with existing code.
func GetTenantID(ctx context.Context) string {
	if t := GetTenant(ctx); t != nil {
		return t.ID
	}
	return ""
}
