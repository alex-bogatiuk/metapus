package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	_cryptoPaymentsTable     = "doc_crypto_payments"
	_cryptoPaymentLinesTable = "doc_crypto_payment_lines"
)

// CryptoPaymentRepo implements crypto_payment.Repository.
type CryptoPaymentRepo struct {
	*BaseDocumentRepo[*crypto_payment.CryptoPayment]
}

// NewCryptoPaymentRepo creates a new crypto payment repository.
func NewCryptoPaymentRepo() *CryptoPaymentRepo {
	repo := &CryptoPaymentRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*crypto_payment.CryptoPayment](
			_cryptoPaymentsTable,
			postgres.ExtractDBColumns[crypto_payment.CryptoPayment](),
			func() *crypto_payment.CryptoPayment { return &crypto_payment.CryptoPayment{} },
		),
	}

	// Register RLS dimensions
	repo.RegisterRLSDimension("merchant", "merchant_id")

	return repo
}

// GetLines retrieves lines for a crypto payment.
func (r *CryptoPaymentRepo) GetLines(ctx context.Context, docID id.ID) ([]crypto_payment.CryptoPaymentLine, error) {
	q := r.Builder().
		Select("line_id", "line_no", "description", "amount").
		From(_cryptoPaymentLinesTable).
		Where(squirrel.Eq{"document_id": docID}).
		OrderBy("line_no")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var lines []crypto_payment.CryptoPaymentLine
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &lines, sql, args...); err != nil {
		return nil, fmt.Errorf("get lines: %w", err)
	}

	return lines, nil
}

// SaveLines saves lines for a crypto payment (delete existing + COPY new).
func (r *CryptoPaymentRepo) SaveLines(ctx context.Context, docID id.ID, lines []crypto_payment.CryptoPaymentLine) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	deleteSQL := "DELETE FROM " + _cryptoPaymentLinesTable + " WHERE document_id = $1"
	if _, err := querier.Exec(ctx, deleteSQL, docID); err != nil {
		return fmt.Errorf("delete existing lines: %w", err)
	}

	if len(lines) == 0 {
		return nil
	}

	columns := []string{"line_id", "document_id", "line_no", "description", "amount"}

	rows := make([][]any, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, []any{
			line.LineID, docID, line.LineNo, line.Description, line.Amount,
		})
	}

	txm := r.getTxManager(ctx)
	inserter := postgres.NewBatchInserter(txm)
	if _, err := inserter.CopyFromSlice(ctx, _cryptoPaymentLinesTable, columns, rows); err != nil {
		return fmt.Errorf("copy lines: %w", err)
	}

	return nil
}

// FindByTxHash finds a crypto payment by blockchain transaction hash.
func (r *CryptoPaymentRepo) FindByTxHash(ctx context.Context, txHash string) (*crypto_payment.CryptoPayment, error) {
	q := r.Builder().Select("*").
		From(_cryptoPaymentsTable).
		Where(squirrel.Eq{"tx_hash": txHash}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var p crypto_payment.CryptoPayment
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &p, sql, args...); err != nil {
		return nil, fmt.Errorf("find by tx hash: %w", err)
	}

	return &p, nil
}

// ListByStatus returns all crypto payments in a given status (e.g. "confirming").
func (r *CryptoPaymentRepo) ListByStatus(ctx context.Context, status crypto_payment.PaymentStatus) ([]*crypto_payment.CryptoPayment, error) {
	q := r.Builder().Select("*").
		From(_cryptoPaymentsTable).
		Where(squirrel.Eq{"status": string(status)}).
		Where(squirrel.Eq{"deletion_mark": false}).
		OrderBy("created_at ASC")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var payments []*crypto_payment.CryptoPayment
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &payments, sql, args...); err != nil {
		return nil, fmt.Errorf("list by status: %w", err)
	}

	return payments, nil
}
