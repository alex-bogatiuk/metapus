package postgres

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

// ContextTxManager resolves the concrete per-tenant transaction manager at
// request time. It lets the composition root inject a stable tx.Manager port
// without leaking tenant-pool lookup into handlers or application services.
type ContextTxManager struct{}

func NewContextTxManager() ContextTxManager { return ContextTxManager{} }

func (ContextTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return fmt.Errorf("resolve tenant transaction manager: %w", err)
	}
	return txm.RunInTransaction(ctx, fn)
}
