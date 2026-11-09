package tenant

import (
	"context"
	"errors"

	"metapus/internal/core/tx"
)

// Context keys for tenant-related values.
type ctxKey int

const (
	txManagerKey ctxKey = iota
	tenantKey
)

// Errors for context operations.
var (
	ErrNoTenantInContext = errors.New("tenant not found in context")
	ErrNoTxManager       = errors.New("transaction manager not found in context")
)

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

// ContextTxManager resolves the tenant transaction manager for each command.
// It stores no tenant or connection between requests.
type ContextTxManager struct{}

func (ContextTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	manager, err := GetTxManager(ctx)
	if err != nil {
		return err
	}
	return manager.RunInTransaction(ctx, fn)
}
