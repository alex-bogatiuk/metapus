package postgres

import (
	"context"
	"fmt"

	"metapus/internal/core/tenant"
)

// MustGetTxManager returns *postgres.TxManager from context.
// It is meant for infrastructure code that needs access to GetQuerier()/GetTx().
//
// Domain code should depend only on internal/core/tx.Manager.
func MustGetTxManager(ctx context.Context) *TxManager {
	txm := tenant.MustGetTxManager(ctx)
	postgresTxm, ok := txm.(*TxManager)
	if !ok || postgresTxm == nil {
		panic(fmt.Sprintf("TxManager in context has unexpected type: %T", txm))
	}
	return postgresTxm
}

