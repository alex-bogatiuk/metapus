package crypto_repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

type CheckoutPageRepo struct{}

func NewCheckoutPageRepo() *CheckoutPageRepo { return &CheckoutPageRepo{} }

func checkoutQuerier(ctx context.Context) postgres.Querier {
	return postgres.MustGetTxManager(ctx).GetQuerier(ctx)
}

func (r *CheckoutPageRepo) GetCheckoutPage(ctx context.Context, publicRef string) (cryptoapp.CheckoutPageData, error) {
	var data cryptoapp.CheckoutPageData
	err := checkoutQuerier(ctx).QueryRow(ctx, `
		SELECT ci.id,ci.status,
		       (ci.status IN ('created','partially_paid') AND (NOT ci.single_payment OR ci.selected_payment_id IS NULL)),
		       COALESCE(w.address,''),ci.expected_amount,ci.received_amount,ci.overpaid_amount,
		       ci.expires_at,COALESCE(ci.order_id,''),COALESCE(ci.description,''),COALESCE(t.symbol,t.code,''),
		       COALESCE(t.name,''),COALESCE(t.decimal_places,6),COALESCE(n.name,''),COALESCE(n.explorer_url,''),
		       COALESCE(n.confirmations_needed,19),COALESCE(m.name,''),
		       COALESCE((SELECT MAX(cp.confirmations) FROM doc_crypto_payments cp WHERE cp.invoice_id=ci.id AND cp.canonicality='canonical' AND cp.status NOT IN ('reorged','replaced','failed') AND cp.deletion_mark=FALSE AND cp._deleted_at IS NULL),0)
		FROM doc_crypto_invoices ci
		LEFT JOIN cat_wallets w ON w.id=ci.wallet_id
		LEFT JOIN cat_tokens t ON t.id=ci.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id=t.network_id
		LEFT JOIN cat_merchants m ON m.id=ci.merchant_id
		WHERE ci.public_ref=$1 AND ci._deleted_at IS NULL
	`, publicRef).Scan(&data.ID, &data.Status, &data.AcceptsPayments, &data.WalletAddress, &data.ExpectedAmount, &data.ReceivedAmount, &data.OverpaidAmount,
		&data.ExpiresAt, &data.OrderID, &data.Description, &data.TokenSymbol, &data.TokenName, &data.DecimalPlaces,
		&data.NetworkName, &data.ExplorerURL, &data.ConfirmationsNeeded, &data.MerchantName, &data.Confirmations)
	if err == pgx.ErrNoRows {
		return data, apperror.NewNotFound("checkout", "reference")
	}
	return data, err
}

func (r *CheckoutPageRepo) GetCheckoutStatus(ctx context.Context, publicRef string) (cryptoapp.CheckoutStatus, error) {
	var status cryptoapp.CheckoutStatus
	err := checkoutQuerier(ctx).QueryRow(ctx, `
		SELECT ci.status,
		       (ci.status IN ('created','partially_paid') AND (NOT ci.single_payment OR ci.selected_payment_id IS NULL)),
		       ci.received_amount,ci.overpaid_amount,
		       COALESCE((SELECT MAX(cp.confirmations) FROM doc_crypto_payments cp WHERE cp.invoice_id=ci.id AND cp.canonicality='canonical' AND cp.status NOT IN ('reorged','replaced','failed') AND cp.deletion_mark=FALSE AND cp._deleted_at IS NULL),0),
		       COALESCE(t.decimal_places,6)
		FROM doc_crypto_invoices ci LEFT JOIN cat_tokens t ON t.id=ci.token_id
		WHERE ci.public_ref=$1 AND ci._deleted_at IS NULL
	`, publicRef).Scan(&status.Status, &status.AcceptsPayments, &status.ReceivedAmount, &status.OverpaidAmount, &status.Confirmations, &status.DecimalPlaces)
	if err == pgx.ErrNoRows {
		return status, apperror.NewNotFound("checkout", "reference")
	}
	return status, err
}

func (r *CheckoutPageRepo) RecordInvoiceViewed(ctx context.Context, invoiceID id.ID) error {
	_, err := checkoutQuerier(ctx).Exec(ctx, `UPDATE doc_crypto_invoices SET view_count=view_count+1,first_viewed_at=COALESCE(first_viewed_at,NOW()),last_viewed_at=NOW() WHERE id=$1`, invoiceID)
	return err
}

func (r *CheckoutPageRepo) LockPaymentLinkForRedemption(ctx context.Context, publicRef string) (cryptoapp.RedeemablePaymentLink, error) {
	var link cryptoapp.RedeemablePaymentLink
	err := checkoutQuerier(ctx).QueryRow(ctx, `
		SELECT id,merchant_id,token_id,amount,description,max_uses,current_uses,status,ttl_minutes
		FROM doc_payment_links WHERE public_ref=$1 AND _deleted_at IS NULL FOR UPDATE
	`, publicRef).Scan(&link.ID, &link.MerchantID, &link.TokenID, &link.Amount, &link.Description, &link.MaxUses, &link.CurrentUses, &link.Status, &link.TTLMinutes)
	if err == pgx.ErrNoRows {
		return link, apperror.NewNotFound("payment_link", publicRef)
	}
	if err != nil {
		return link, fmt.Errorf("lock payment link for redemption: %w", err)
	}
	return link, nil
}

func (r *CheckoutPageRepo) MarkPaymentLinkRedeemed(ctx context.Context, linkID id.ID) (bool, error) {
	tag, err := checkoutQuerier(ctx).Exec(ctx, `
		UPDATE doc_payment_links SET current_uses=current_uses+1,updated_at=NOW(),
		status=CASE WHEN max_uses>0 AND current_uses+1>=max_uses THEN 'disabled' ELSE status END
		WHERE id=$1 AND status='active' AND _deleted_at IS NULL AND (max_uses=0 OR current_uses<max_uses)
	`, linkID)
	return tag.RowsAffected() == 1, err
}
