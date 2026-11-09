package tenantdb

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

type poolContextKey struct{}

var poolKey poolContextKey
var ErrNoPoolInContext = errors.New("database pool not found in context")

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
