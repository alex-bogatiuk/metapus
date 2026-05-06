package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	_cryptoInvoicesTable     = "doc_crypto_invoices"
	_cryptoInvoiceLinesTable = "doc_crypto_invoice_lines"
)

// CryptoInvoiceRepo implements crypto_invoice.Repository.
// List() is inherited from BaseDocumentRepo (universal filter engine).
type CryptoInvoiceRepo struct {
	*BaseDocumentRepo[*crypto_invoice.CryptoInvoice]
}

// NewCryptoInvoiceRepo creates a new crypto invoice repository.
func NewCryptoInvoiceRepo() *CryptoInvoiceRepo {
	repo := &CryptoInvoiceRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*crypto_invoice.CryptoInvoice](
			_cryptoInvoicesTable,
			postgres.ExtractDBColumns[crypto_invoice.CryptoInvoice](),
			func() *crypto_invoice.CryptoInvoice { return &crypto_invoice.CryptoInvoice{} },
		),
	}

	// Register RLS dimensions
	repo.RegisterRLSDimension("merchant", "merchant_id")

	return repo
}

// GetLines retrieves lines for a crypto invoice.
func (r *CryptoInvoiceRepo) GetLines(ctx context.Context, docID id.ID) ([]crypto_invoice.CryptoInvoiceLine, error) {
	q := r.Builder().
		Select("line_id", "line_no", "description", "amount").
		From(_cryptoInvoiceLinesTable).
		Where(squirrel.Eq{"document_id": docID}).
		OrderBy("line_no")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var lines []crypto_invoice.CryptoInvoiceLine
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &lines, sql, args...); err != nil {
		return nil, fmt.Errorf("get lines: %w", err)
	}

	return lines, nil
}

// SaveLines saves lines for a crypto invoice (delete existing + COPY new).
func (r *CryptoInvoiceRepo) SaveLines(ctx context.Context, docID id.ID, lines []crypto_invoice.CryptoInvoiceLine) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	// Delete existing lines
	deleteSQL := "DELETE FROM " + _cryptoInvoiceLinesTable + " WHERE document_id = $1"
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
	if _, err := inserter.CopyFromSlice(ctx, _cryptoInvoiceLinesTable, columns, rows); err != nil {
		return fmt.Errorf("copy lines: %w", err)
	}

	return nil
}

// FindByExternalID finds a crypto invoice by external idempotency key.
func (r *CryptoInvoiceRepo) FindByExternalID(ctx context.Context, externalID string) (*crypto_invoice.CryptoInvoice, error) {
	q := r.Builder().Select("*").
		From(_cryptoInvoicesTable).
		Where(squirrel.Eq{"external_id": externalID}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var inv crypto_invoice.CryptoInvoice
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &inv, sql, args...); err != nil {
		return nil, fmt.Errorf("find by external id: %w", err)
	}

	return &inv, nil
}

// ExpireOverdue implements crypto_invoice.Repository.
// Marks invoices past their expires_at as Expired AND releases their leased wallets.
//
// Uses two separate queries (not a CTE) because:
//  1. RowsAffected must return invoice count, not wallet count
//  2. Document repo should not embed catalog table schema in a CTE
//
// Both queries execute on the same connection (via TxManager), so they see
// each other's writes and are atomic if wrapped in a transaction by the caller.
func (r *CryptoInvoiceRepo) ExpireOverdue(ctx context.Context) (int64, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	// Step 1: Expire invoices and capture their IDs.
	rows, err := querier.Query(ctx, `
		UPDATE `+_cryptoInvoicesTable+`
		SET status = $1, version = version + 1, updated_at = NOW()
		WHERE status IN ($2, $3)
		  AND expires_at < NOW()
		  AND deletion_mark = FALSE
		RETURNING id
	`,
		crypto_invoice.InvoiceStatusExpired,
		crypto_invoice.InvoiceStatusCreated,
		crypto_invoice.InvoiceStatusPartiallyPaid,
	)
	if err != nil {
		return 0, fmt.Errorf("expire overdue invoices: %w", err)
	}
	defer rows.Close()

	expiredIDs := make([]id.ID, 0, 8)
	for rows.Next() {
		var invoiceID id.ID
		if err := rows.Scan(&invoiceID); err != nil {
			return 0, fmt.Errorf("scan expired invoice id: %w", err)
		}
		expiredIDs = append(expiredIDs, invoiceID)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate expired invoices: %w", err)
	}

	if len(expiredIDs) == 0 {
		return 0, nil
	}

	// Step 2: Release wallets leased for the expired invoices.
	// Uses wallet.WalletStatusFree directly — no magic numbers.
	_, err = querier.Exec(ctx, `
		UPDATE cat_wallets
		SET status = $1,
		    leased_for_id = NULL,
		    leased_until = NULL,
		    version = version + 1,
		    updated_at = NOW()
		WHERE leased_for_id = ANY($2)
	`,
		wallet.WalletStatusFree,
		expiredIDs,
	)
	if err != nil {
		return 0, fmt.Errorf("release wallets for expired invoices: %w", err)
	}

	return int64(len(expiredIDs)), nil
}
