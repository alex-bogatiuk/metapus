package portal_repo

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/dto"
)

// ── Invoice Detail ─────────────────────────────────────────────────────

// GetInvoiceDetail returns full invoice detail including wallet address and expiry.
// Scope-filtered: only returns invoices belonging to the merchant scope.
func (r *DashboardRepo) GetInvoiceDetail(ctx context.Context, invoiceID id.ID, activeMerchantID *id.ID) (*dto.PortalInvoiceDetailResponse, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	const sqlQuery = `
		SELECT i.id, i.number, i.status, i.expected_amount, i.received_amount,
			t.symbol, COALESCE(n.name, '') AS network, t.decimal_places,
			i.created_at, i.expires_at,
			COALESCE(i.external_id, '') AS external_id,
			COALESCE(i.customer_email, '') AS customer_email,
			COALESCE(i.description, '') AS description,
			COALESCE(i.order_id, '') AS order_id,
			COALESCE(w.address, '') AS wallet_address,
			p.tx_hash, p.from_address,
			p.fee_fixed, p.fee_percent_bp, p.fee_min, p.fee_max,
			p.amount AS payment_amount,
			p.confirmed_at
		FROM doc_crypto_invoices i
		JOIN cat_tokens t ON t.id = i.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		LEFT JOIN cat_wallets w ON w.id = i.wallet_id
		LEFT JOIN LATERAL (
			SELECT tx_hash, from_address,
				fee_fixed, fee_percent_bp, fee_min, fee_max,
				amount, confirmed_at
			FROM doc_crypto_payments
			WHERE invoice_id = i.id AND _deleted_at IS NULL
			ORDER BY created_at DESC
			LIMIT 1
		) p ON true
		WHERE i.id = $1
			AND i.merchant_id = ANY($2)
			AND i._deleted_at IS NULL`

	var item dto.PortalInvoiceDetailResponse
	var iid id.ID
	var amount, received int64
	var createdAt, expiresAt time.Time
	var externalID, customerEmail string
	var txHash, fromAddress *string
	var feeFixed, feeMin, feeMax, paymentAmount *int64
	var feePercentBP *int
	var confirmedAt *time.Time

	if err := q.QueryRow(ctx, sqlQuery, invoiceID, ids).Scan(
		&iid, &item.Number, &item.Status,
		&amount, &received,
		&item.Symbol, &item.Network, &item.DecimalPlaces,
		&createdAt, &expiresAt,
		&externalID, &customerEmail,
		&item.Description, &item.OrderID,
		&item.WalletAddress,
		&txHash, &fromAddress,
		&feeFixed, &feePercentBP, &feeMin, &feeMax,
		&paymentAmount,
		&confirmedAt,
	); err != nil {
		return nil, fmt.Errorf("portal invoice detail: %w", err)
	}

	item.ID = iid.String()
	item.Amount = strconv.FormatInt(amount, 10)
	item.ReceivedAmount = strconv.FormatInt(received, 10)
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.ExpiresAt = expiresAt.Format(time.RFC3339)
	item.ExternalID = externalID
	item.CustomerEmail = customerEmail

	if txHash != nil {
		item.TxHash = *txHash
	}
	if fromAddress != nil {
		item.FromAddress = *fromAddress
	}
	if confirmedAt != nil {
		ts := confirmedAt.Format(time.RFC3339)
		item.ConfirmedAt = &ts
	}

	// Calculate processing fee + net (same formula as ListInvoices)
	if paymentAmount != nil && feeFixed != nil && feePercentBP != nil {
		fee := calculateFee(*feeFixed, *feePercentBP, safeDeref(feeMin), safeDeref(feeMax), *paymentAmount)
		item.ProcessingFee = strconv.FormatInt(fee, 10)
		net := *paymentAmount - fee
		if net < 0 {
			net = 0
		}
		item.NetAmount = strconv.FormatInt(net, 10)
	}

	// Fetch timeline events
	timeline, err := r.getInvoiceTimeline(ctx, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("portal invoice timeline: %w", err)
	}
	item.Timeline = timeline

	// Fetch webhook deliveries
	deliveries, err := r.getInvoiceWebhookDeliveries(ctx, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("portal invoice webhook deliveries: %w", err)
	}
	item.WebhookDeliveries = deliveries

	return &item, nil
}

// getInvoiceTimeline returns FSM transition events for the invoice's payment.
func (r *DashboardRepo) getInvoiceTimeline(ctx context.Context, invoiceID id.ID) ([]dto.PortalTimelineEvent, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT pe.id, pe.event_type, pe.from_status, pe.to_status, pe.metadata, pe.created_at
		FROM reg_crypto_payment_events pe
		JOIN doc_crypto_payments p ON p.id = pe.payment_id
		WHERE p.invoice_id = $1 AND p._deleted_at IS NULL
		ORDER BY pe.created_at ASC`

	rows, err := q.Query(ctx, sqlQuery, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("query timeline: %w", err)
	}
	defer rows.Close()

	events := make([]dto.PortalTimelineEvent, 0, 8)
	for rows.Next() {
		var event dto.PortalTimelineEvent
		var eid id.ID
		var metadataJSON []byte
		var createdAt time.Time

		if err := rows.Scan(
			&eid, &event.EventType, &event.FromStatus, &event.ToStatus,
			&metadataJSON, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan timeline event: %w", err)
		}

		event.ID = eid.String()
		event.CreatedAt = createdAt.Format(time.RFC3339)

		// Parse metadata JSON into structured fields.
		if len(metadataJSON) > 0 {
			var raw map[string]interface{}
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
func (r *DashboardRepo) getInvoiceWebhookDeliveries(ctx context.Context, invoiceID id.ID) ([]dto.PortalWebhookDeliveryItem, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT id, event_type, delivery_id, status_code, response_time_ms,
			attempt, error_message, created_at
		FROM sys_webhook_deliveries
		WHERE invoice_id = $1
		ORDER BY created_at DESC
		LIMIT 50`

	rows, err := q.Query(ctx, sqlQuery, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("query webhook deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]dto.PortalWebhookDeliveryItem, 0, 8)
	for rows.Next() {
		var d dto.PortalWebhookDeliveryItem
		var did id.ID
		var createdAt time.Time

		if err := rows.Scan(
			&did, &d.EventType, &d.DeliveryID, &d.StatusCode,
			&d.ResponseTimeMs, &d.Attempt, &d.ErrorMessage, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan webhook delivery: %w", err)
		}

		d.ID = did.String()
		d.CreatedAt = createdAt.Format(time.RFC3339)
		deliveries = append(deliveries, d)
	}

	return deliveries, rows.Err()
}

// ── Withdrawals (read-only) ────────────────────────────────────────────

// WithdrawalFilter contains filter parameters for the withdrawal list.
type WithdrawalFilter struct {
	Status string // withdrawal status filter
	Sort   string // column to sort by
	Order  string // "asc" or "desc"
}

// ListWithdrawals returns a page of withdrawals for the scoped merchants.
func (r *DashboardRepo) ListWithdrawals(ctx context.Context, activeMerchantID *id.ID, filter WithdrawalFilter, limit, offset int) ([]dto.PortalWithdrawalItem, int, error) {
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
		return []dto.PortalWithdrawalItem{}, 0, nil
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
			t.symbol, COALESCE(n.name, '') AS network, t.decimal_places,
			w.dest_address, COALESCE(w.tx_hash, '') AS tx_hash,
			w.created_at
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

	items := make([]dto.PortalWithdrawalItem, 0, limit)
	for rows.Next() {
		var item dto.PortalWithdrawalItem
		var wid id.ID
		var amount, networkFee int64
		var createdAt time.Time

		if err := rows.Scan(
			&wid, &item.Number, &item.Status, &amount, &networkFee,
			&item.Symbol, &item.Network, &item.DecimalPlaces,
			&item.DestAddress, &item.TxHash,
			&createdAt,
		); err != nil {
			return nil, 0, fmt.Errorf("portal withdrawals scan: %w", err)
		}

		item.ID = wid.String()
		item.Amount = strconv.FormatInt(amount, 10)
		if networkFee > 0 {
			item.NetworkFee = strconv.FormatInt(networkFee, 10)
		}
		item.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, item)
	}

	return items, total, rows.Err()
}

// GetWebhookDeliveriesByMerchant returns recent webhook deliveries for a merchant.
// Used by the webhook management page.
func (r *DashboardRepo) GetWebhookDeliveriesByMerchant(ctx context.Context, merchantID id.ID, limit, offset int) ([]dto.PortalWebhookDeliveryItem, int, error) {
	q := querier(ctx)

	// Count
	var total int
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_webhook_deliveries WHERE merchant_id = $1
	`, merchantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("webhook deliveries count: %w", err)
	}

	if total == 0 {
		return []dto.PortalWebhookDeliveryItem{}, 0, nil
	}

	rows, err := q.Query(ctx, `
		SELECT id, event_type, delivery_id, status_code, response_time_ms,
			attempt, error_message, created_at
		FROM sys_webhook_deliveries
		WHERE merchant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, merchantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("webhook deliveries list: %w", err)
	}
	defer rows.Close()

	items := make([]dto.PortalWebhookDeliveryItem, 0, limit)
	for rows.Next() {
		var d dto.PortalWebhookDeliveryItem
		var did id.ID
		var createdAt time.Time

		if err := rows.Scan(
			&did, &d.EventType, &d.DeliveryID, &d.StatusCode,
			&d.ResponseTimeMs, &d.Attempt, &d.ErrorMessage, &createdAt,
		); err != nil {
			return nil, 0, fmt.Errorf("webhook deliveries scan: %w", err)
		}

		d.ID = did.String()
		d.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, d)
	}

	return items, total, rows.Err()
}

// GetMerchantWebhookSecret reads the webhook secret from merchant attributes.
func (r *DashboardRepo) GetMerchantWebhookSecret(ctx context.Context, merchantID id.ID) (webhookURL, webhookSecret string, err error) {
	q := querier(ctx)

	err = q.QueryRow(ctx, `
		SELECT
			COALESCE(attributes->>'webhookUrl', '')    AS webhook_url,
			COALESCE(attributes->>'webhookSecret', '') AS webhook_secret
		FROM cat_merchants
		WHERE id = $1 AND _deleted_at IS NULL
	`, merchantID).Scan(&webhookURL, &webhookSecret)
	if err != nil {
		return "", "", fmt.Errorf("get merchant webhook config: %w", err)
	}

	return webhookURL, webhookSecret, nil
}
