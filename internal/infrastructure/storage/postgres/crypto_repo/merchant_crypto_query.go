package crypto_repo

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/tariff"
	"metapus/internal/infrastructure/storage/postgres"
)

type MerchantCryptoQueryRepo struct{}

func NewMerchantCryptoQueryRepo() *MerchantCryptoQueryRepo { return &MerchantCryptoQueryRepo{} }

func merchantCryptoQuerier(ctx context.Context) postgres.Querier {
	return postgres.MustGetTxManager(ctx).GetQuerier(ctx)
}

func (r *MerchantCryptoQueryRepo) ResolveTokenContext(ctx context.Context, code string) (cryptoapp.MerchantTokenContext, bool, error) {
	var value cryptoapp.MerchantTokenContext
	err := merchantCryptoQuerier(ctx).QueryRow(ctx, `
		SELECT t.id,n.id,n.code,t.decimal_places,n.confirmations_needed,n.finality_mode,COALESCE(p.enabled,FALSE),
		       EXISTS(SELECT 1 FROM reg_crypto_network_holds h WHERE h.network_id=n.id AND h.status='open')
		FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id=t.network_id
		LEFT JOIN sys_crypto_network_policies p ON p.network_id=n.id AND p._deleted_at IS NULL
		WHERE t.code=$1 AND t.deletion_mark=FALSE AND t.is_active=TRUE
		  AND n.deletion_mark=FALSE AND n.is_active=TRUE LIMIT 1
	`, code).Scan(&value.TokenID, &value.NetworkID, &value.NetworkCode, &value.DecimalPlaces, &value.RequiredConfirmations, &value.FinalityMode, &value.Enabled, &value.Held)
	if err == pgx.ErrNoRows {
		return value, false, nil
	}
	return value, err == nil, err
}

func (r *MerchantCryptoQueryRepo) ListMerchantCurrencies(ctx context.Context) ([]cryptoapp.MerchantCurrency, error) {
	rows, err := merchantCryptoQuerier(ctx).Query(ctx, `
		SELECT t.code, t.symbol, t.name, n.code, n.name, t.token_standard, t.decimal_places,
		       t.contract_address, n.confirmations_needed, n.finality_mode,
		       (COALESCE(p.enabled, FALSE) AND NOT EXISTS(
		           SELECT 1 FROM reg_crypto_network_holds h WHERE h.network_id = n.id AND h.status = 'open'
		       )) AS accepts_payments
		FROM cat_tokens t
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		LEFT JOIN sys_crypto_network_policies p ON p.network_id = n.id AND p._deleted_at IS NULL
		WHERE t.deletion_mark = FALSE AND t.is_active = TRUE AND t._deleted_at IS NULL
		  AND n.deletion_mark = FALSE AND n.is_active = TRUE AND n._deleted_at IS NULL
		ORDER BY t.code ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query merchant currencies: %w", err)
	}
	defer rows.Close()
	items := make([]cryptoapp.MerchantCurrency, 0, 8)
	for rows.Next() {
		var item cryptoapp.MerchantCurrency
		if err := rows.Scan(
			&item.Code, &item.Symbol, &item.Name, &item.Network, &item.NetworkName,
			&item.Standard, &item.DecimalPlaces, &item.ContractAddress,
			&item.RequiredConfirmations, &item.FinalityMode, &item.AcceptsPayments,
		); err != nil {
			return nil, fmt.Errorf("scan merchant currency: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant currencies: %w", err)
	}
	return items, nil
}

func (r *MerchantCryptoQueryRepo) GetMerchantInvoiceDisplay(ctx context.Context, invoiceID id.ID) (cryptoapp.MerchantInvoiceDisplay, error) {
	var value cryptoapp.MerchantInvoiceDisplay
	err := merchantCryptoQuerier(ctx).QueryRow(ctx, `
		SELECT COALESCE(w.address,''),COALESCE(t.code,''),COALESCE(n.code,''),COALESCE(t.decimal_places,0)
		FROM doc_crypto_invoices ci
		LEFT JOIN cat_wallets w ON w.id=ci.wallet_id
		LEFT JOIN cat_tokens t ON t.id=ci.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id=t.network_id
		WHERE ci.id=$1
	`, invoiceID).Scan(&value.WalletAddress, &value.TokenCode, &value.NetworkCode, &value.DecimalPlaces)
	return value, err
}

func (r *MerchantCryptoQueryRepo) GetMerchantInvoiceObservedAt(ctx context.Context, invoiceID id.ID) (*time.Time, error) {
	var observedAt *time.Time
	err := merchantCryptoQuerier(ctx).QueryRow(ctx, `SELECT MAX(updated_at) FROM doc_crypto_payments WHERE invoice_id=$1 AND deletion_mark=FALSE`, invoiceID).Scan(&observedAt)
	return observedAt, err
}

func (r *MerchantCryptoQueryRepo) ListMerchantInvoicePayments(ctx context.Context, invoiceID id.ID) ([]cryptoapp.MerchantInvoicePayment, error) {
	byInvoice, err := r.ListMerchantInvoicePaymentsByInvoices(ctx, []id.ID{invoiceID})
	if err != nil {
		return nil, err
	}
	return byInvoice[invoiceID], nil
}

func (r *MerchantCryptoQueryRepo) ListMerchantInvoicePaymentsByInvoices(ctx context.Context, invoiceIDs []id.ID) (map[id.ID][]cryptoapp.MerchantInvoicePayment, error) {
	result := make(map[id.ID][]cryptoapp.MerchantInvoicePayment, len(invoiceIDs))
	if len(invoiceIDs) == 0 {
		return result, nil
	}
	rows, err := merchantCryptoQuerier(ctx).Query(ctx, `
		SELECT id, invoice_id, tx_hash, event_index, event_kind, amount, fee_fixed, fee_percent_bp, fee_min, fee_max,
		       status, canonicality, finality_status, posted, confirmations, required_confs,
		       source_nonce, source_address, replacement_tx_hash, detected_at, confirmed_at, updated_at
		FROM doc_crypto_payments
		WHERE invoice_id = ANY($1) AND deletion_mark = FALSE AND _deleted_at IS NULL
		ORDER BY detected_at ASC, id ASC
	`, invoiceIDs)
	if err != nil {
		return nil, fmt.Errorf("query merchant invoice payments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item cryptoapp.MerchantInvoicePayment
		var rawAmount, feeFixed, feeMin, feeMax types.RawAmount
		var feePercentBP int
		if err := rows.Scan(
			&item.ID, &item.InvoiceID, &item.TxHash, &item.EventIndex, &item.EventKind,
			&rawAmount, &feeFixed, &feePercentBP, &feeMin, &feeMax,
			&item.Status, &item.Canonicality, &item.FinalityStatus, &item.Posted,
			&item.Confirmations, &item.RequiredConfirmations, &item.SourceNonce,
			&item.SourceAddress, &item.ReplacementTxHash, &item.DetectedAt, &item.ConfirmedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan merchant invoice payment: %w", err)
		}
		fee, net := tariff.Fee{Fixed: feeFixed, PercentBP: feePercentBP, Min: feeMin, Max: feeMax}.Split(rawAmount)
		item.RawAmount = rawAmount
		item.RawFeeAmount = fee
		item.RawNetAmount = net
		item.Amount = rawAmount.String()
		item.FeeAmount = fee.String()
		item.NetAmount = net.String()
		result[item.InvoiceID] = append(result[item.InvoiceID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant invoice payments: %w", err)
	}
	return result, nil
}

func (r *MerchantCryptoQueryRepo) ListMerchantInvoices(ctx context.Context, filter cryptoapp.MerchantInvoiceListFilter) ([]cryptoapp.MerchantInvoiceRecord, error) {
	where := `ci.merchant_id = $1 AND ci.deletion_mark = FALSE AND ci._deleted_at IS NULL`
	args := make([]any, 0, 12)
	args = append(args, filter.MerchantID)
	nextArg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	if len(filter.Statuses) > 0 {
		where += ` AND ci.status = ANY(` + nextArg(filter.Statuses) + `)`
	}
	if filter.Currency != "" {
		where += ` AND t.code = ` + nextArg(filter.Currency)
	}
	if filter.OrderID != "" {
		where += ` AND ci.order_id = ` + nextArg(filter.OrderID)
	}
	if filter.ExternalID != "" {
		where += ` AND ci.external_id = ` + nextArg(filter.ExternalID)
	}
	if filter.CustomerID != "" {
		where += ` AND ci.customer_id = ` + nextArg(filter.CustomerID)
	}
	if filter.CreatedFrom != nil {
		where += ` AND ci.created_at >= ` + nextArg(*filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		where += ` AND ci.created_at < ` + nextArg(*filter.CreatedTo)
	}
	if filter.CompletedFrom != nil {
		where += ` AND ci.completed_at >= ` + nextArg(*filter.CompletedFrom)
	}
	if filter.CompletedTo != nil {
		where += ` AND ci.completed_at < ` + nextArg(*filter.CompletedTo)
	}

	orderBy := `ci.created_at DESC, ci.id DESC`
	if filter.SortByCompleted {
		where += ` AND ci.completed_at IS NOT NULL`
		if filter.CursorTime != nil && filter.CursorID != nil {
			tsPlaceholder := nextArg(*filter.CursorTime)
			idPlaceholder := nextArg(*filter.CursorID)
			where += ` AND (ci.completed_at < ` + tsPlaceholder + ` OR (ci.completed_at = ` + tsPlaceholder + ` AND ci.id < ` + idPlaceholder + `))`
		}
		orderBy = `ci.completed_at DESC, ci.id DESC`
	} else if filter.CursorTime != nil && filter.CursorID != nil {
		tsPlaceholder := nextArg(*filter.CursorTime)
		idPlaceholder := nextArg(*filter.CursorID)
		where += ` AND (ci.created_at < ` + tsPlaceholder + ` OR (ci.created_at = ` + tsPlaceholder + ` AND ci.id < ` + idPlaceholder + `))`
	}

	limitPlaceholder := nextArg(filter.Limit + 1)

	query := `
		SELECT ci.id, ci.public_ref, ci.version, ci.status, ci.single_payment, ci.selected_payment_id,
		       ci.expected_amount, ci.received_amount, ci.confirmed_amount, ci.credited_amount, ci.overpaid_amount,
		       ci.required_confirmations, ci.finality_mode, ci.expires_at,
		       ci.order_id, ci.external_id, ci.customer_id, ci.description,
		       ci.notification_url, ci.return_url, ci.metadata,
		       ci.paid_at, ci.confirmed_at, ci.completed_at, ci.reversed_at, ci.created_at,
		       COALESCE(w.address, ''), COALESCE(t.code, ''), COALESCE(n.code, ''), COALESCE(t.decimal_places, 0)
		FROM doc_crypto_invoices ci
		LEFT JOIN cat_wallets w ON w.id = ci.wallet_id
		LEFT JOIN cat_tokens t ON t.id = ci.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE ` + where + `
		ORDER BY ` + orderBy + `
		LIMIT ` + limitPlaceholder

	rows, err := merchantCryptoQuerier(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query merchant invoices: %w", err)
	}
	defer rows.Close()

	records := make([]cryptoapp.MerchantInvoiceRecord, 0, filter.Limit+1)
	for rows.Next() {
		inv := &crypto_invoice.CryptoInvoice{}
		var rec cryptoapp.MerchantInvoiceRecord
		var statusStr string
		var metadataRaw []byte

		if err := rows.Scan(
			&inv.ID, &inv.PublicRef, &inv.Version, &statusStr, &inv.SinglePayment, &inv.SelectedPaymentID,
			&inv.ExpectedAmount, &inv.ReceivedAmount, &inv.ConfirmedAmount, &inv.CreditedAmount, &inv.OverpaidAmount,
			&inv.RequiredConfirmations, &inv.FinalityMode, &inv.ExpiresAt,
			&inv.OrderID, &inv.ExternalID, &inv.CustomerID, &inv.Description,
			&inv.NotificationURL, &inv.ReturnURL, &metadataRaw,
			&inv.PaidAt, &inv.ConfirmedAt, &inv.CompletedAt, &inv.ReversedAt, &inv.CreatedAt,
			&rec.WalletAddress, &rec.TokenCode, &rec.NetworkCode, &rec.DecimalPlaces,
		); err != nil {
			return nil, fmt.Errorf("scan merchant invoice: %w", err)
		}
		inv.Status = crypto_invoice.InvoiceStatus(statusStr)
		inv.Metadata = map[string]string{}
		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &inv.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal merchant invoice metadata: %w", err)
			}
		}
		rec.Invoice = inv
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant invoices: %w", err)
	}
	return records, nil
}
