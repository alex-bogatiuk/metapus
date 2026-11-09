package document_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_invoice"
)

var _ crypto_invoice.ReferenceRepository = (*CryptoInvoiceRepo)(nil)

func (r *CryptoInvoiceRepo) GetMerchantInvoiceForUpdate(ctx context.Context, merchantID, invoiceID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	q := r.Builder().Select(r.selectCols...).From(crypto_invoice.Definition.Table).
		Where(squirrel.Eq{"id": invoiceID, "merchant_id": merchantID, "_deleted_at": nil, "deletion_mark": false}).
		Suffix("FOR UPDATE")
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build merchant invoice lock: %w", err)
	}
	var invoice crypto_invoice.CryptoInvoice
	if err := pgxscan.Get(ctx, r.getTxManager(ctx).GetQuerier(ctx), &invoice, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("invoice", invoiceID.String())
		}
		return nil, fmt.Errorf("lock merchant invoice: %w", err)
	}
	return &invoice, nil
}

// The system classification and linked payment identify top-ups independently
// of merchant references, lifecycle status and chain reorgs.
func (r *CryptoInvoiceRepo) IsPersistentTopUp(ctx context.Context, merchantID, invoiceID id.ID) (bool, error) {
	var topUp bool
	err := r.getTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM doc_crypto_invoices i
			JOIN cat_wallets w ON w.id=i.wallet_id
			WHERE i.id=$1 AND i.merchant_id=$2 AND i._deleted_at IS NULL AND i.deletion_mark=FALSE
			  AND i.invoice_kind='top_up' AND i.creation_source='system'
			  AND w.allocation_mode='persistent'
			  AND w.merchant_id=i.merchant_id
			  AND EXISTS (
				SELECT 1 FROM doc_crypto_payments p
				WHERE p.invoice_id=i.id AND p.merchant_id=i.merchant_id
				  AND p.wallet_id=i.wallet_id AND p._deleted_at IS NULL
				  AND p.token_id=i.token_id AND p.network_id=w.network_id
			  )
		)`, invoiceID, merchantID).Scan(&topUp)
	if err != nil {
		return false, fmt.Errorf("identify persistent top-up: %w", err)
	}
	return topUp, nil
}

func (r *CryptoInvoiceRepo) UpdateMerchantReference(ctx context.Context, in crypto_invoice.ReferenceUpdate) (crypto_invoice.ReferenceUpdateResult, error) {
	q := r.Builder().Update(crypto_invoice.Definition.Table).
		Set("order_id", in.OrderID).Set("external_id", in.ExternalID).
		Set("version", squirrel.Expr("version + 1")).Set("updated_at", squirrel.Expr("NOW()")).
		Where(squirrel.Eq{"id": in.InvoiceID, "merchant_id": in.MerchantID, "version": in.Version, "_deleted_at": nil, "deletion_mark": false}).
		Suffix("RETURNING version, updated_at")
	if in.Metadata != nil {
		q = q.Set("metadata", in.Metadata)
	}
	sql, args, err := q.ToSql()
	if err != nil {
		return crypto_invoice.ReferenceUpdateResult{}, fmt.Errorf("build invoice reference update: %w", err)
	}
	var result crypto_invoice.ReferenceUpdateResult
	err = r.getTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, sql, args...).Scan(&result.Version, &result.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			field := ""
			switch pgErr.ConstraintName {
			case "idx_crypto_invoices_merchant_order_id":
				field = "orderId"
			case "idx_crypto_invoices_merchant_external_id":
				field = "externalId"
			}
			if field != "" {
				return result, apperror.NewConflict(field+" is already used by another invoice of this merchant").WithDetail("field", field)
			}
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return result, apperror.NewConcurrentModification(crypto_invoice.Definition.Table, in.InvoiceID)
		}
		return result, fmt.Errorf("update invoice reference: %w", err)
	}
	return result, nil
}
