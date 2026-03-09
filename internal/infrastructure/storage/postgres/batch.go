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

// CopyFromRows performs bulk insert using PostgreSQL COPY protocol.
// columns: list of column names
// rows: channel of row values (each row is []any matching columns)
//
// Example:
//
//	rows := make(chan []any, 100)
//	go func() {
//	    for _, item := range items {
//	        rows <- []any{item.ID, item.Name, item.Quantity}
//	    }
//	    close(rows)
//	}()
//	err := inserter.CopyFromRows(ctx, "cat_nomenclature", []string{"id", "name", "quantity"}, rows)
func (b *BatchInserter) CopyFromRows(ctx context.Context, table string, columns []string, rows <-chan []any) (int64, error) {
	tx := b.txManager.GetTx(ctx)
	if tx == nil {
		return 0, fmt.Errorf("CopyFromRows requires transaction context")
	}
	
	// Create CopyFrom source
	source := &channelCopyFromSource{
		columns: columns,
		rows:    rows,
	}
	
	return tx.CopyFrom(ctx, pgx.Identifier{table}, columns, source)
}

// CopyFromSlice performs bulk insert from a slice of rows.
func (b *BatchInserter) CopyFromSlice(ctx context.Context, table string, columns []string, rows [][]any) (int64, error) {
	tx := b.txManager.GetTx(ctx)
	if tx == nil {
		return 0, fmt.Errorf("CopyFromSlice requires transaction context")
	}
	
	return tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromRows(rows))
}

// channelCopyFromSource implements pgx.CopyFromSource for channel-based row streaming.
type channelCopyFromSource struct {
	columns []string
	rows    <-chan []any
	current []any
	err     error
}

func (s *channelCopyFromSource) Next() bool {
	row, ok := <-s.rows
	if !ok {
		return false
	}
	s.current = row
	return true
}

func (s *channelCopyFromSource) Values() ([]any, error) {
	return s.current, nil
}

func (s *channelCopyFromSource) Err() error {
	return s.err
}

// BatchExecutor provides batch query execution.
type BatchExecutor struct {
	txManager *TxManager
}

// NewBatchExecutor creates a new batch executor.
func NewBatchExecutor(txManager *TxManager) *BatchExecutor {
	return &BatchExecutor{txManager: txManager}
}

// BatchQuery represents a query in a batch.
type BatchQuery struct {
	SQL  string
	Args []any
}

// ExecuteBatch executes multiple queries in a single round-trip.
func (e *BatchExecutor) ExecuteBatch(ctx context.Context, queries []BatchQuery) error {
	tx := e.txManager.GetTx(ctx)
	if tx == nil {
		return fmt.Errorf("ExecuteBatch requires transaction context")
	}
	
	batch := &pgx.Batch{}
	for _, q := range queries {
		batch.Queue(q.SQL, q.Args...)
	}
	
	results := tx.SendBatch(ctx, batch)
	defer results.Close()
	
	for range queries {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch query failed: %w", err)
		}
	}
	
	return nil
}
