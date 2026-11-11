package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// BatchInserter provides efficient bulk insert operations using COPY protocol.
// Significantly faster than individual INSERTs for large datasets (1000+ rows).
type BatchInserter struct {
	txManager *TxManager
}

// NewBatchInserter creates a new batch inserter.
func NewBatchInserter(txManager *TxManager) *BatchInserter {
	return &BatchInserter{txManager: txManager}
}

// CopyFromSlice performs bulk insert from a slice of rows.
func (b *BatchInserter) CopyFromSlice(ctx context.Context, table string, columns []string, rows [][]any) (int64, error) {
	tx := b.txManager.GetTx(ctx)
	if tx == nil {
		return 0, fmt.Errorf("CopyFromSlice requires transaction context")
	}

	return tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromRows(rows))
}
