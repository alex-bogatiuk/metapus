package portal_repo

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/postgres"
	"time"

	"metapus/internal/configuration/crypto/application/portalapp"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/types"
)

// ── Invoice Detail ─────────────────────────────────────────────────────

// GetInvoiceDetail returns full invoice detail including wallet address and expiry.
// Scope-filtered: only returns invoices belonging to the merchant scope.
func (r *DashboardRepo) GetInvoiceDetail(ctx context.Context, invoiceID id.ID, activeMerchantID *id.ID) (*portalapp.PortalInvoiceDetailResponse, error) {
	var detail *portalapp.PortalInvoiceDetailResponse
	err := postgres.ReadSnapshot(ctx, func(snapshotCtx context.Context) error {
		var err error
		detail, err = r.getInvoiceDetailSnapshot(snapshotCtx, invoiceID, activeMerchantID)
		return err
	})
	return detail, err
}

func (r *DashboardRepo) getInvoiceDetailSnapshot(ctx context.Context, invoiceID id.ID, activeMerchantID *id.ID) (*portalapp.PortalInvoiceDetailResponse, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	const sqlQuery = `
		SELECT i.id, i.public_ref, i.number, i.status, i.expected_amount, i.received_amount,
			t.symbol, COALESCE(n.name, '') AS network,
			COALESCE(n.explorer_url, '') AS explorer_url, t.decimal_places,
			i.created_at, i.expires_at,
			(i.status IN ('created','partially_paid') AND (NOT i.single_payment OR i.selected_payment_id IS NULL)),
			COALESCE(i.external_id, '') AS external_id,
			COALESCE(i.customer_id, '') AS customer_id,
			COALESCE(i.description, '') AS description,
			COALESCE(i.order_id, '') AS order_id,
			COALESCE(w.address, '') AS wallet_address,
			COALESCE(i.invoice_kind, 'payment') AS invoice_kind,
   i.merchant_id,i.token_id,i.single_payment,i.selected_payment_id IS NOT NULL,transaction_timestamp()
		FROM doc_crypto_invoices i
		JOIN cat_tokens t ON t.id = i.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		LEFT JOIN cat_wallets w ON w.id = i.wallet_id
		WHERE i.id = $1
			AND i.merchant_id = ANY($2)
			AND i._deleted_at IS NULL`

	var item portalapp.PortalInvoiceDetailResponse
	var iid, merchantID, tokenID id.ID
	var singlePayment, selectedPayment bool
	var observedAt time.Time
	var amount, received types.RawAmount
	var createdAt, expiresAt time.Time
	var externalID, customerID, invoiceKind string

	if err := q.QueryRow(ctx, sqlQuery, invoiceID, ids).Scan(
		&iid, &item.CheckoutRef, &item.Number, &item.Status,
		&amount, &received,
		&item.Symbol, &item.Network, &item.ExplorerURL, &item.DecimalPlaces,
		&createdAt, &expiresAt, &item.AcceptsPayments,
		&externalID, &customerID,
		&item.Description, &item.OrderID,
		&item.WalletAddress,
		&invoiceKind, &merchantID, &tokenID, &singlePayment, &selectedPayment, &observedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.NewNotFound("invoice", invoiceID.String())
		}
		return nil, fmt.Errorf("portal invoice detail: %w", err)
	}

	item.ID = iid.String()
	item.Amount = amount.String()
	item.ReceivedAmount = received.String()
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.ExpiresAt = expiresAt.Format(time.RFC3339)
	item.ExternalID = externalID
	item.CustomerID = customerID
	item.InvoiceKind = invoiceKind
	item.ObservedAt = observedAt.Format(time.RFC3339Nano)
	item.PaymentAcceptanceReason = invoicePaymentAcceptanceReason(item.Status, item.AcceptsPayments, singlePayment, selectedPayment, expiresAt, observedAt)
	item.CanViewWebhooks = merchant.MustGetPortalScope(ctx).AllowsFor(merchantID, merchant.PortalRoleManager)

	payments, err := r.loadInvoicePayments(ctx, []id.ID{invoiceID}, true)
	if err != nil {
		return nil, err
	}
	applyPaymentSummary(&item.PortalInvoiceItem, payments[item.ID])
	item.Payments = payments[item.ID].Payments
	item.ReceivedAmount = payments[item.ID].Received.String()
	ledgers, ledger, err := r.loadInvoiceLedger(ctx, invoiceID, merchantID, tokenID)
	if err != nil {
		return nil, err
	}
	item.FinancialSummary = portalapp.PortalInvoiceFinancialSummary{ConfirmedAmount: payments[item.ID].Confirmed.String(), PendingNetAmount: payments[item.ID].PendingNet.String(), Ledger: ledger}
	for i := range item.Payments {
		paymentLedger, ok := ledgers[item.Payments[i].ID]
		if !ok {
			paymentLedger = inconsistentInvoiceLedger()
		}
		item.Payments[i].Ledger = paymentLedger
	}

	// Fetch timeline events
	timeline, err := r.getInvoiceTimeline(ctx, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("portal invoice timeline: %w", err)
	}
	item.Timeline = timeline

	item.WebhookDeliveries = []portalapp.PortalWebhookDeliveryItem{}
	if item.CanViewWebhooks {
		deliveries, total, err := r.getInvoiceWebhookDeliveries(ctx, invoiceID, merchantID)
		if err != nil {
			return nil, fmt.Errorf("portal invoice webhook deliveries: %w", err)
		}
		item.WebhookDeliveries = deliveries
		item.WebhookDeliveryTotal = total
	}

	return &item, nil
}

// getInvoiceTimeline returns FSM transition events for the invoice's payment.
func (r *DashboardRepo) getInvoiceTimeline(ctx context.Context, invoiceID id.ID) ([]portalapp.PortalTimelineEvent, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT pe.id, pe.payment_id, pe.event_type, pe.from_status, pe.to_status, pe.metadata, pe.created_at
		FROM reg_crypto_payment_events pe
		JOIN doc_crypto_payments p ON p.id = pe.payment_id
		WHERE p.invoice_id = $1 AND p._deleted_at IS NULL
		ORDER BY pe.created_at ASC, pe.id ASC`

	rows, err := q.Query(ctx, sqlQuery, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("query timeline: %w", err)
	}
	defer rows.Close()

	events := make([]portalapp.PortalTimelineEvent, 0, 8)
	for rows.Next() {
		var event portalapp.PortalTimelineEvent
		var eid, paymentID id.ID
		var metadataJSON []byte
		var createdAt time.Time

		if err := rows.Scan(
			&eid, &paymentID, &event.EventType, &event.FromStatus, &event.ToStatus,
			&metadataJSON, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan timeline event: %w", err)
		}

		event.ID = eid.String()
		event.PaymentID = paymentID.String()
		event.CreatedAt = createdAt.Format(time.RFC3339)

		// Parse metadata JSON into structured fields.
		if len(metadataJSON) > 0 {
			var raw map[string]any
			if err := json.Unmarshal(metadataJSON, &raw); err == nil {
				if v, ok := raw["confirmations"]; ok {
					if n, ok := v.(float64); ok {
						event.Metadata.Confirmations = int(n)
					}
				}
				if v, ok := raw["requiredConfs"]; ok {
					if n, ok := v.(float64); ok {
						event.Metadata.RequiredConfs = int(n)
					}
				}
				if v, ok := raw["blockNumber"]; ok {
					if n, ok := v.(float64); ok {
						event.Metadata.BlockNumber = int64(n)
					}
				}
				if v, ok := raw["txHash"]; ok {
					if s, ok := v.(string); ok {
						event.Metadata.TxHash = s
					}
				}
			}
		}

		events = append(events, event)
	}

	return events, rows.Err()
}

// getInvoiceWebhookDeliveries returns webhook delivery attempts for an invoice.
func (r *DashboardRepo) getInvoiceWebhookDeliveries(ctx context.Context, invoiceID, merchantID id.ID) ([]portalapp.PortalWebhookDeliveryItem, int, error) {
	q := querier(ctx)
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM sys_webhook_deliveries WHERE invoice_id=$1 AND merchant_id=$2`, invoiceID, merchantID).Scan(&total); err != nil {
		return nil, 0, err
	}

	const sqlQuery = `
		SELECT id, event_type, delivery_id, status_code, response_time_ms,
			attempt, error_message, created_at
		FROM sys_webhook_deliveries
		WHERE invoice_id = $1 AND merchant_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 50`

	rows, err := q.Query(ctx, sqlQuery, invoiceID, merchantID)
	if err != nil {
		return nil, 0, fmt.Errorf("query webhook deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]portalapp.PortalWebhookDeliveryItem, 0, 8)
	for rows.Next() {
		var d portalapp.PortalWebhookDeliveryItem
		var did id.ID
		var createdAt time.Time

		if err := rows.Scan(
			&did, &d.EventType, &d.DeliveryID, &d.StatusCode,
			&d.ResponseTimeMs, &d.Attempt, &d.ErrorMessage, &createdAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan webhook delivery: %w", err)
		}

		d.ID = did.String()
		d.CreatedAt = createdAt.Format(time.RFC3339)
		deliveries = append(deliveries, d)
	}

	return deliveries, total, rows.Err()
}

// ── Withdrawals (read-only) ────────────────────────────────────────────

// WithdrawalFilter contains filter parameters for the withdrawal list.
type WithdrawalFilter struct {
	Status string // withdrawal status filter
	Sort   string // column to sort by
	Order  string // "asc" or "desc"
}

// ListWithdrawals returns a page of withdrawals for the scoped merchants.
func (r *DashboardRepo) ListWithdrawals(ctx context.Context, activeMerchantID *id.ID, filter WithdrawalFilter, limit, offset int) ([]portalapp.PortalWithdrawalItem, int, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	// ── Build WHERE clause ──
	where := `w.merchant_id = ANY($1) AND w._deleted_at IS NULL`
	args := []any{ids}
	argIdx := 2

	if filter.Status != "" {
		where += fmt.Sprintf(` AND w.status = $%d`, argIdx)
		args = append(args, filter.Status)
		argIdx++
	}

	// ── Count total ──
	countQ := `SELECT COUNT(*) FROM doc_crypto_withdrawals w WHERE ` + where
	var total int
	if err := q.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("portal withdrawals count: %w", err)
	}

	if total == 0 {
		return []portalapp.PortalWithdrawalItem{}, 0, nil
	}

	// ── Sort mapping ──
	sortCol := "w.created_at"
	switch filter.Sort {
	case "number":
		sortCol = "w.number"
	case "status":
		sortCol = "w.status"
	case "amount":
		sortCol = "w.amount"
	}
	orderDir := "DESC"
	if filter.Order == "asc" {
		orderDir = "ASC"
	}

	// ── Fetch page ──
	dataQ := fmt.Sprintf(`
		SELECT w.id, w.number, w.status, w.amount, w.network_fee,
			t.symbol, COALESCE(n.name, '') AS network,
			COALESCE(n.explorer_url, '') AS explorer_url, t.decimal_places,
			w.dest_address, COALESCE(w.tx_hash, '') AS tx_hash,
			w.created_at,(SELECT plan.risk_class FROM reg_crypto_aml_withdrawal_lines line JOIN reg_crypto_aml_withdrawal_plans plan ON plan.request_id=line.request_id JOIN reg_crypto_signing_authorizations auth ON auth.basis_type='CryptoAMLWithdrawalLine' AND auth.basis_id=line.id AND auth.generation=1 AND auth.attempt=1 JOIN reg_crypto_aml_first_dispatches first ON first.authorization_id=auth.id WHERE line.withdrawal_id=w.id)
		FROM doc_crypto_withdrawals w
		JOIN cat_tokens t ON t.id = w.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE %s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d`,
		where, sortCol, orderDir, argIdx, argIdx+1,
	)
	args = append(args, limit, offset)

	rows, err := q.Query(ctx, dataQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("portal withdrawals: %w", err)
	}
	defer rows.Close()

	items := make([]portalapp.PortalWithdrawalItem, 0, limit)
	for rows.Next() {
		var item portalapp.PortalWithdrawalItem
		var wid id.ID
		var amount, networkFee types.RawAmount
		var createdAt time.Time

		if err := rows.Scan(
			&wid, &item.Number, &item.Status, &amount, &networkFee,
			&item.Symbol, &item.Network, &item.ExplorerURL, &item.DecimalPlaces,
			&item.DestAddress, &item.TxHash,
			&createdAt, &item.RiskClass,
		); err != nil {
			return nil, 0, fmt.Errorf("portal withdrawals scan: %w", err)
		}

		item.ID = wid.String()
		item.Amount = amount.String()
		if networkFee.IsPositive() {
			item.NetworkFee = networkFee.String()
		}
		item.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, item)
	}

	return items, total, rows.Err()
}
