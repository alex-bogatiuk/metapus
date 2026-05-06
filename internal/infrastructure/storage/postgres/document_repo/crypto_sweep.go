package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	_cryptoSweepsTable     = "doc_crypto_sweeps"
	_cryptoSweepLinesTable = "doc_crypto_sweep_lines"
)

// CryptoSweepRepo implements crypto_sweep.Repository.
type CryptoSweepRepo struct {
	*BaseDocumentRepo[*crypto_sweep.CryptoSweep]
}

// NewCryptoSweepRepo creates a new crypto sweep repository.
func NewCryptoSweepRepo() *CryptoSweepRepo {
	repo := &CryptoSweepRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*crypto_sweep.CryptoSweep](
			_cryptoSweepsTable,
			postgres.ExtractDBColumns[crypto_sweep.CryptoSweep](),
			func() *crypto_sweep.CryptoSweep { return &crypto_sweep.CryptoSweep{} },
		),
	}
	// System document — no RLS dimensions (admin-only)
	return repo
}

// GetLines retrieves lines for a crypto sweep.
func (r *CryptoSweepRepo) GetLines(ctx context.Context, docID id.ID) ([]crypto_sweep.CryptoSweepLine, error) {
	q := r.Builder().
		Select("line_id", "line_no", "wallet_id", "amount", "network_fee", "tx_hash", "confirmed").
		From(_cryptoSweepLinesTable).
		Where(squirrel.Eq{"document_id": docID}).
		OrderBy("line_no")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var lines []crypto_sweep.CryptoSweepLine
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &lines, sql, args...); err != nil {
		return nil, fmt.Errorf("get lines: %w", err)
	}
	return lines, nil
}

// SaveLines saves lines for a crypto sweep.
func (r *CryptoSweepRepo) SaveLines(ctx context.Context, docID id.ID, lines []crypto_sweep.CryptoSweepLine) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	deleteSQL := "DELETE FROM " + _cryptoSweepLinesTable + " WHERE document_id = $1"
	if _, err := querier.Exec(ctx, deleteSQL, docID); err != nil {
		return fmt.Errorf("delete existing lines: %w", err)
	}
	if len(lines) == 0 {
		return nil
	}

	columns := []string{"line_id", "document_id", "line_no", "wallet_id", "amount", "network_fee", "tx_hash", "confirmed"}
	rows := make([][]any, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, []any{
			line.LineID, docID, line.LineNo, line.WalletID,
			line.Amount, line.NetworkFee, line.TxHash, line.Confirmed,
		})
	}

	txm := r.getTxManager(ctx)
	inserter := postgres.NewBatchInserter(txm)
	if _, err := inserter.CopyFromSlice(ctx, _cryptoSweepLinesTable, columns, rows); err != nil {
		return fmt.Errorf("copy lines: %w", err)
	}
	return nil
}
