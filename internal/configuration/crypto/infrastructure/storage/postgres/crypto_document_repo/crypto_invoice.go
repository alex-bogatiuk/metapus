package crypto_document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/document_repo"
)

// CryptoInvoiceRepo implements crypto_invoice.Repository.
// List is inherited from BaseDocumentRepo's universal filter engine.
type CryptoInvoiceRepo struct {
	*document_repo.BaseDocumentRepo[*crypto_invoice.CryptoInvoice]
}

func NewCryptoInvoiceRepo() *CryptoInvoiceRepo {
	repo := &CryptoInvoiceRepo{
		BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition[*crypto_invoice.CryptoInvoice](crypto_invoice.Definition,
			postgres.ExtractDBColumns[crypto_invoice.CryptoInvoice](),
			func() *crypto_invoice.CryptoInvoice { return &crypto_invoice.CryptoInvoice{} },
		),
	}
	repo.RegisterRLSDimension("merchant", "merchant_id")
	return repo
}

func (r *CryptoInvoiceRepo) GetByIDForUpdate(ctx context.Context, docID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	q := r.Builder().Select(r.SelectColumns()...).
		From(crypto_invoice.Definition.Table).
		Where(squirrel.Eq{"id": docID}).
		Suffix("FOR UPDATE")
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}
	var invoice crypto_invoice.CryptoInvoice
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &invoice, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound(crypto_invoice.Definition.Table, docID.String())
		}
		return nil, fmt.Errorf("get invoice for update: %w", err)
	}
	return &invoice, nil
}

func (r *CryptoInvoiceRepo) FindByExternalID(ctx context.Context, externalID string) (*crypto_invoice.CryptoInvoice, error) {
	q := r.Builder().Select("*").From(crypto_invoice.Definition.Table).
		Where(squirrel.Eq{"external_id": externalID}).
		Where(squirrel.Eq{"deletion_mark": false}).Limit(1)
	return r.findOne(ctx, q, "external id")
}

func (r *CryptoInvoiceRepo) FindByPublicRef(ctx context.Context, publicRef string) (*crypto_invoice.CryptoInvoice, error) {
	q := r.Builder().Select("*").From(crypto_invoice.Definition.Table).
		Where(squirrel.Eq{"public_ref": publicRef}).
		Where(squirrel.Eq{"deletion_mark": false}).Limit(1)
	return r.findOne(ctx, q, "public ref")
}

func (r *CryptoInvoiceRepo) FindByMerchantOrderID(ctx context.Context, merchantID id.ID, orderID string) (*crypto_invoice.CryptoInvoice, error) {
	return r.findMerchantInvoice(ctx, squirrel.Eq{"merchant_id": merchantID, "order_id": orderID}, "order id")
}

func (r *CryptoInvoiceRepo) FindByMerchantExternalID(ctx context.Context, merchantID id.ID, externalID string) (*crypto_invoice.CryptoInvoice, error) {
	return r.findMerchantInvoice(ctx, squirrel.Eq{"merchant_id": merchantID, "external_id": externalID}, "external id")
}

func (r *CryptoInvoiceRepo) findMerchantInvoice(ctx context.Context, conditions squirrel.Eq, label string) (*crypto_invoice.CryptoInvoice, error) {
	q := r.Builder().Select("*").From(crypto_invoice.Definition.Table).
		Where(conditions).Where(squirrel.Eq{"deletion_mark": false}).Limit(1)
	return r.findOne(ctx, q, label)
}

func (r *CryptoInvoiceRepo) findOne(ctx context.Context, query squirrel.SelectBuilder, label string) (*crypto_invoice.CryptoInvoice, error) {
	sql, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}
	var invoice crypto_invoice.CryptoInvoice
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &invoice, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("find invoice by %s: %w", label, err)
	}
	return &invoice, nil
}

// FindOverdueIDs is read-only. Each candidate is revalidated under locks by
// InvoiceLifecycleService, so races with a just-arrived payment are harmless.
func (r *CryptoInvoiceRepo) FindOverdueIDs(ctx context.Context, limit int) ([]id.ID, error) {
	if limit < 1 {
		limit = 500
	}
	rows, err := r.GetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT id FROM doc_crypto_invoices
		WHERE status IN ($1,$2) AND expires_at<NOW() AND deletion_mark=FALSE
		  AND (single_payment=FALSE OR selected_payment_id IS NULL)
		ORDER BY expires_at,id LIMIT $3
	`, crypto_invoice.InvoiceStatusCreated, crypto_invoice.InvoiceStatusPartiallyPaid, limit)
	if err != nil {
		return nil, fmt.Errorf("find overdue invoices: %w", err)
	}
	defer rows.Close()
	result := make([]id.ID, 0, limit)
	for rows.Next() {
		var invoiceID id.ID
		if err := rows.Scan(&invoiceID); err != nil {
			return nil, err
		}
		result = append(result, invoiceID)
	}
	return result, rows.Err()
}

func (r *CryptoInvoiceRepo) ListActiveMerchantInvoiceIDs(ctx context.Context, merchantID id.ID) ([]id.ID, error) {
	rows, err := r.GetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT id FROM doc_crypto_invoices
		WHERE merchant_id=$1 AND status IN ('created','partially_paid','paid')
		  AND single_payment=FALSE AND deletion_mark=FALSE
		ORDER BY id
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list active merchant invoices: %w", err)
	}
	defer rows.Close()
	var ids []id.ID
	for rows.Next() {
		var invoiceID id.ID
		if err := rows.Scan(&invoiceID); err != nil {
			return nil, err
		}
		ids = append(ids, invoiceID)
	}
	return ids, rows.Err()
}

func (r *CryptoInvoiceRepo) FirstTwoPaymentIDs(ctx context.Context, invoiceID id.ID) ([]id.ID, error) {
	rows, err := r.GetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT id FROM doc_crypto_payments
		WHERE invoice_id=$1 AND deletion_mark=FALSE
		  AND status NOT IN ('reorged','replaced','failed') AND canonicality<>'orphaned'
		ORDER BY created_at,id LIMIT 2
	`, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("list invoice payments: %w", err)
	}
	defer rows.Close()
	ids := make([]id.ID, 0, 2)
	for rows.Next() {
		var paymentID id.ID
		if err := rows.Scan(&paymentID); err != nil {
			return nil, err
		}
		ids = append(ids, paymentID)
	}
	return ids, rows.Err()
}

// LockWalletForInvoice is called after InvoiceLifecycleService has locked the
// invoice row, preserving the global invoice -> wallet order.
func (r *CryptoInvoiceRepo) LockWalletForInvoice(ctx context.Context, invoiceID id.ID) error {
	rows, err := r.GetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT w.id FROM cat_wallets w
		JOIN doc_crypto_invoices ci ON ci.wallet_id=w.id
		WHERE ci.id=$1
		ORDER BY w.id FOR UPDATE
	`, invoiceID)
	if err != nil {
		return fmt.Errorf("lock invoice wallet: %w", err)
	}
	rows.Close()
	return rows.Err()
}

func (r *CryptoInvoiceRepo) ReleaseInvoiceWallet(ctx context.Context, invoiceID id.ID, closeReason string) error {
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if _, err := querier.Exec(ctx, `
		UPDATE cat_wallets w
		SET status=CASE WHEN $3='confirmed' AND EXISTS (
		      SELECT 1 FROM doc_crypto_invoices i
		      JOIN cat_tokens t ON t.id=i.token_id
		      LEFT JOIN reg_merchant_token_config c ON c.merchant_id=i.merchant_id AND c.token_id=i.token_id
		      WHERE i.id=$2 AND i.wallet_id=w.id
		        AND COALESCE(c.sweep_threshold,t.sweep_threshold)=0
		        AND i.credited_amount>COALESCE(c.sweep_retain_amount,t.sweep_retain_amount)
		    ) THEN 'sweep_pending' ELSE $1 END,
		    leased_for_id=NULL,leased_until=NULL,
		    cooldown_until=NOW()+make_interval(secs=>COALESCE((
		      SELECT p.address_reuse_cooldown_seconds FROM sys_crypto_wallet_pool_policies p
		      WHERE p.network_id=w.network_id AND p._deleted_at IS NULL
		    ),0)),version=w.version+1,updated_at=NOW()
		WHERE w.leased_for_id=$2
	`, wallet.WalletStatusFree, invoiceID, closeReason); err != nil {
		return fmt.Errorf("release expired invoice wallet: %w", err)
	}
	if _, err := querier.Exec(ctx, `
		UPDATE reg_crypto_wallet_allocations a
		SET closed_at=NOW(),cooldown_until=COALESCE(w.cooldown_until,NOW()),
		    close_reason=$2,version=a.version+1
		FROM cat_wallets w
		WHERE a.invoice_id=$1 AND a.wallet_id=w.id AND a.closed_at IS NULL AND a._deleted_at IS NULL
	`, invoiceID, closeReason); err != nil {
		return fmt.Errorf("close invoice wallet allocation: %w", err)
	}
	return nil
}

func (r *CryptoInvoiceRepo) CleanupOrphanedExpiredLeases(ctx context.Context) error {
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if _, err := querier.Exec(ctx, `
		WITH candidates AS (
		  SELECT w.id FROM cat_wallets w
		  WHERE w.status=$1 AND w.allocation_mode=$2 AND w.leased_until<NOW()
		    AND w.leased_for_id IS NOT NULL
		    AND NOT EXISTS(SELECT 1 FROM doc_crypto_invoices i WHERE i.id=w.leased_for_id)
		  ORDER BY w.id FOR UPDATE SKIP LOCKED
		)
		UPDATE cat_wallets w
		SET status=$3,leased_for_id=NULL,leased_until=NULL,
		    cooldown_until=NOW()+make_interval(secs=>COALESCE((
		      SELECT p.address_reuse_cooldown_seconds FROM sys_crypto_wallet_pool_policies p
		      WHERE p.network_id=w.network_id AND p._deleted_at IS NULL
		    ),0)),version=w.version+1,updated_at=NOW()
		FROM candidates c WHERE w.id=c.id
	`, wallet.WalletStatusLeased, wallet.AllocationModeTransient, wallet.WalletStatusFree); err != nil {
		return fmt.Errorf("release orphaned invoice wallet leases: %w", err)
	}
	if _, err := querier.Exec(ctx, `
		UPDATE reg_crypto_wallet_allocations a
		SET closed_at=NOW(),cooldown_until=COALESCE(w.cooldown_until,NOW()),
		    close_reason='orphaned_lease',version=a.version+1
		FROM cat_wallets w
		WHERE a.wallet_id=w.id AND a.closed_at IS NULL AND a._deleted_at IS NULL
		  AND w.allocation_mode=$1
		  AND NOT EXISTS(SELECT 1 FROM doc_crypto_invoices i WHERE i.id=a.invoice_id)
	`, wallet.AllocationModeTransient); err != nil {
		return fmt.Errorf("close orphaned wallet allocations: %w", err)
	}
	return nil
}

var _ crypto_invoice.Repository = (*CryptoInvoiceRepo)(nil)
