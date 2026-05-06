package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	_cryptoWithdrawalsTable     = "doc_crypto_withdrawals"
	_cryptoWithdrawalLinesTable = "doc_crypto_withdrawal_lines"
)

// CryptoWithdrawalRepo implements crypto_withdrawal.Repository.
type CryptoWithdrawalRepo struct {
	*BaseDocumentRepo[*crypto_withdrawal.CryptoWithdrawal]
}

// NewCryptoWithdrawalRepo creates a new crypto withdrawal repository.
func NewCryptoWithdrawalRepo() *CryptoWithdrawalRepo {
	repo := &CryptoWithdrawalRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*crypto_withdrawal.CryptoWithdrawal](
			_cryptoWithdrawalsTable,
			postgres.ExtractDBColumns[crypto_withdrawal.CryptoWithdrawal](),
			func() *crypto_withdrawal.CryptoWithdrawal { return &crypto_withdrawal.CryptoWithdrawal{} },
		),
	}
	repo.RegisterRLSDimension("merchant", "merchant_id")
	return repo
}

// GetLines retrieves lines for a crypto withdrawal.
func (r *CryptoWithdrawalRepo) GetLines(ctx context.Context, docID id.ID) ([]crypto_withdrawal.CryptoWithdrawalLine, error) {
	q := r.Builder().
		Select("line_id", "line_no", "description", "amount").
		From(_cryptoWithdrawalLinesTable).
		Where(squirrel.Eq{"document_id": docID}).
		OrderBy("line_no")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var lines []crypto_withdrawal.CryptoWithdrawalLine
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &lines, sql, args...); err != nil {
		return nil, fmt.Errorf("get lines: %w", err)
	}
	return lines, nil
}

// SaveLines saves lines for a crypto withdrawal.
func (r *CryptoWithdrawalRepo) SaveLines(ctx context.Context, docID id.ID, lines []crypto_withdrawal.CryptoWithdrawalLine) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	deleteSQL := "DELETE FROM " + _cryptoWithdrawalLinesTable + " WHERE document_id = $1"
	if _, err := querier.Exec(ctx, deleteSQL, docID); err != nil {
		return fmt.Errorf("delete existing lines: %w", err)
	}
	if len(lines) == 0 {
		return nil
	}

	columns := []string{"line_id", "document_id", "line_no", "description", "amount"}
	rows := make([][]any, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, []any{line.LineID, docID, line.LineNo, line.Description, line.Amount})
	}

	txm := r.getTxManager(ctx)
	inserter := postgres.NewBatchInserter(txm)
	if _, err := inserter.CopyFromSlice(ctx, _cryptoWithdrawalLinesTable, columns, rows); err != nil {
		return fmt.Errorf("copy lines: %w", err)
	}
	return nil
}

// FindPending returns withdrawals in Created status for processing.
func (r *CryptoWithdrawalRepo) FindPending(ctx context.Context, limit int) ([]*crypto_withdrawal.CryptoWithdrawal, error) {
	q := r.Builder().Select("*").
		From(_cryptoWithdrawalsTable).
		Where(squirrel.Eq{"status": crypto_withdrawal.WithdrawalStatusCreated}).
		Where(squirrel.Eq{"deletion_mark": false}).
		OrderBy("created_at ASC").
		Limit(uint64(limit))

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var withdrawals []*crypto_withdrawal.CryptoWithdrawal
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &withdrawals, sql, args...); err != nil {
		return nil, fmt.Errorf("find pending: %w", err)
	}
	return withdrawals, nil
}
