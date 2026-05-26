// Package portal_repo provides database access for the merchant portal.
package portal_repo

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// DashboardRepo provides read-only queries for the portal dashboard.
// Architectural contract: every method extracts MerchantScope from context
// and filters by scope.MerchantIDs — no escape from isolation.
type DashboardRepo struct{}

// NewDashboardRepo creates a new portal dashboard repository.
func NewDashboardRepo() *DashboardRepo {
	return &DashboardRepo{}
}

func querier(ctx context.Context) postgres.Querier {
	return postgres.MustGetTxManager(ctx).GetQuerier(ctx)
}

// ScopeIDs returns merchant IDs from context. If activeMerchantID is provided
// and belongs to the scope, returns only that one ID; otherwise returns all.
func (r *DashboardRepo) ScopeIDs(ctx context.Context, activeMerchantID *id.ID) []id.ID {
	return scopeIDs(ctx, activeMerchantID)
}

// scopeIDs returns merchant IDs from context. If activeMerchantID is provided
// and belongs to the scope, returns only that one ID; otherwise returns all.
func scopeIDs(ctx context.Context, activeMerchantID *id.ID) []id.ID {
	scope := appctx.MustGetMerchantScope(ctx)
	if activeMerchantID != nil {
		if slices.Contains(scope.MerchantIDs, *activeMerchantID) {
			return []id.ID{*activeMerchantID}
		}
		// activeMerchantID not in scope — fall through to all scope IDs
	}
	// Defensive copy (invariant §2.13)
	ids := make([]id.ID, len(scope.MerchantIDs))
	copy(ids, scope.MerchantIDs)
	return ids
}

// GetSummary returns aggregate invoice statistics for the scoped merchants.
func (r *DashboardRepo) GetSummary(ctx context.Context, activeMerchantID *id.ID) (*dto.PortalSummaryResponse, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	const sql = `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'confirmed') AS paid,
			COUNT(*) FILTER (WHERE status IN ('created','partially_paid')) AS pending,
			COALESCE(SUM(received_amount) FILTER (WHERE status = 'confirmed'), 0) AS total_received_minor,
			COALESCE(SUM(received_amount) FILTER (WHERE status = 'confirmed' AND created_at >= NOW() - INTERVAL '24 hours'), 0) AS received_24h,
			COALESCE(SUM(received_amount) FILTER (WHERE status = 'confirmed' AND created_at >= NOW() - INTERVAL '48 hours' AND created_at < NOW() - INTERVAL '24 hours'), 0) AS received_prev_24h
		FROM doc_crypto_invoices
		WHERE merchant_id = ANY($1)
			AND _deleted_at IS NULL`

	var total, paid, pending int
	var totalMinor, received24h, receivedPrev24h int64
	err := q.QueryRow(ctx, sql, ids).Scan(
		&total, &paid, &pending,
		&totalMinor, &received24h, &receivedPrev24h,
	)
	if err != nil {
		return nil, fmt.Errorf("portal summary: %w", err)
	}

	// Calculate 24h change percentage
	change := "0.00"
	if receivedPrev24h > 0 {
		pct := float64(received24h-receivedPrev24h) / float64(receivedPrev24h) * 100
		if pct > 0 {
			change = "+" + strconv.FormatFloat(pct, 'f', 2, 64)
		} else {
			change = strconv.FormatFloat(pct, 'f', 2, 64)
		}
	} else if received24h > 0 {
		change = "+100.00"
	}

	return &dto.PortalSummaryResponse{
		TotalInvoices:   total,
		PaidInvoices:    paid,
		PendingInvoices: pending,
		TotalMinorUnits: strconv.FormatInt(totalMinor, 10),
		Change24hPct:    change,
	}, nil
}

// GetCurrencies returns currency breakdown for the scoped merchants.
func (r *DashboardRepo) GetCurrencies(ctx context.Context, activeMerchantID *id.ID) ([]dto.PortalCurrencyItem, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	const sql = `
		SELECT t.id::text AS token_id, t.symbol,
			t.network_id::text AS network_id, COALESCE(n.name, '') AS network,
			t.decimal_places,
			COUNT(i.id) AS cnt,
			COALESCE(SUM(i.received_amount), 0) AS total_minor,
			ROUND(COALESCE(SUM(i.received_amount), 0) * 100.0 /
				NULLIF(SUM(SUM(i.received_amount)) OVER (), 0), 2) AS share_pct
		FROM doc_crypto_invoices i
		JOIN cat_tokens t ON t.id = i.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE i.merchant_id = ANY($1)
			AND i.status = 'confirmed'
			AND i._deleted_at IS NULL
		GROUP BY t.id, t.symbol, t.network_id, n.name, t.decimal_places
		ORDER BY total_minor DESC`

	rows, err := q.Query(ctx, sql, ids)
	if err != nil {
		return nil, fmt.Errorf("portal currencies: %w", err)
	}
	defer rows.Close()

	items := make([]dto.PortalCurrencyItem, 0, 8)
	for rows.Next() {
		var item dto.PortalCurrencyItem
		var totalMinor int64
		var sharePct float64
		if err := rows.Scan(
			&item.TokenID, &item.Symbol, &item.NetworkID, &item.Network, &item.DecimalPlaces,
			&item.Count, &totalMinor, &sharePct,
		); err != nil {
			return nil, fmt.Errorf("portal currencies scan: %w", err)
		}
		item.TotalMinor = strconv.FormatInt(totalMinor, 10)
		item.SharePct = strconv.FormatFloat(sharePct, 'f', 2, 64)
		items = append(items, item)
	}

	return items, rows.Err()
}

// GetChart returns daily deposit volumes for the scoped merchants.
func (r *DashboardRepo) GetChart(ctx context.Context, period string, activeMerchantID *id.ID) ([]dto.PortalChartPoint, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	// Map period string to interval — parameterised to prevent SQL injection.
	interval := "30 days"
	switch period {
	case "7d":
		interval = "7 days"
	case "90d":
		interval = "90 days"
	}

	query := fmt.Sprintf(`
		SELECT DATE_TRUNC('day', created_at)::date AS day,
			COALESCE(SUM(received_amount) FILTER (WHERE status = 'confirmed'), 0) AS deposits
		FROM doc_crypto_invoices
		WHERE merchant_id = ANY($1)
			AND created_at >= NOW() - INTERVAL '%s'
			AND _deleted_at IS NULL
		GROUP BY day
		ORDER BY day`, interval)

	rows, err := q.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("portal chart: %w", err)
	}
	defer rows.Close()

	points := make([]dto.PortalChartPoint, 0, 30)
	for rows.Next() {
		var day time.Time
		var deposits int64
		if err := rows.Scan(&day, &deposits); err != nil {
			return nil, fmt.Errorf("portal chart scan: %w", err)
		}
		points = append(points, dto.PortalChartPoint{
			Day:      day.Format("2006-01-02"),
			Deposits: strconv.FormatInt(deposits, 10),
		})
	}

	return points, rows.Err()
}

// ListMerchants returns the merchants available in the scope.
func (r *DashboardRepo) ListMerchants(ctx context.Context) ([]dto.PortalMerchantItem, error) {
	scope := appctx.MustGetMerchantScope(ctx)
	q := querier(ctx)

	const sql = `
		SELECT id, name, code
		FROM cat_merchants
		WHERE id = ANY($1)
			AND _deleted_at IS NULL
		ORDER BY name`

	rows, err := q.Query(ctx, sql, scope.MerchantIDs)
	if err != nil {
		return nil, fmt.Errorf("portal merchants: %w", err)
	}
	defer rows.Close()

	items := make([]dto.PortalMerchantItem, 0, len(scope.MerchantIDs))
	for rows.Next() {
		var item dto.PortalMerchantItem
		var mid id.ID
		if err := rows.Scan(&mid, &item.Name, &item.Code); err != nil {
			return nil, fmt.Errorf("portal merchants scan: %w", err)
		}
		item.ID = mid.String()
		items = append(items, item)
	}
	return items, rows.Err()
}

// InvoiceFilter contains filter parameters for the invoice list.
type InvoiceFilter struct {
	Status   string // invoice status filter
	Search   string // free-text search (number, externalId, customerEmail)
	TokenID  string // token UUID filter
	DateFrom string // ISO 8601 date (inclusive)
	DateTo   string // ISO 8601 date (inclusive, end of day)
	Sort     string // column to sort by (validated in handler)
	Order    string // "asc" or "desc" (validated in handler)
}

// ListInvoices returns a page of invoices for the scoped merchants.
// Joins with doc_crypto_payments to provide txHash, fee, and net amount.
func (r *DashboardRepo) ListInvoices(ctx context.Context, activeMerchantID *id.ID, filter InvoiceFilter, limit, offset int) ([]dto.PortalInvoiceItem, int, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	// ── Build WHERE clause dynamically ──
	where := `i.merchant_id = ANY($1) AND i._deleted_at IS NULL`
	args := []any{ids}
	argIdx := 2

	if filter.Status != "" {
		where += fmt.Sprintf(` AND i.status = $%d`, argIdx)
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.Search != "" {
		pattern := "%" + filter.Search + "%"
		where += fmt.Sprintf(` AND (i.number ILIKE $%d OR i.external_id ILIKE $%d OR i.customer_email ILIKE $%d)`, argIdx, argIdx, argIdx)
		args = append(args, pattern)
		argIdx++
	}
	if filter.TokenID != "" {
		tokenID, err := id.Parse(filter.TokenID)
		if err == nil {
			where += fmt.Sprintf(` AND i.token_id = $%d`, argIdx)
			args = append(args, tokenID)
			argIdx++
		}
	}
	if filter.DateFrom != "" {
		where += fmt.Sprintf(` AND i.created_at >= $%d::timestamptz`, argIdx)
		args = append(args, filter.DateFrom)
		argIdx++
	}
	if filter.DateTo != "" {
		where += fmt.Sprintf(` AND i.created_at < ($%d::date + 1)::timestamptz`, argIdx)
		args = append(args, filter.DateTo)
		argIdx++
	}

	// ── Count total ──
	countQ := `SELECT COUNT(*) FROM doc_crypto_invoices i WHERE ` + where
	var total int
	if err := q.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("portal invoices count: %w", err)
	}

	// ── Sort mapping ──
	sortCol := "i.created_at"
	switch filter.Sort {
	case "number":
		sortCol = "i.number"
	case "status":
		sortCol = "i.status"
	case "amount":
		sortCol = "i.expected_amount"
	case "received_amount":
		sortCol = "i.received_amount"
	}
	orderDir := "DESC"
	if filter.Order == "asc" {
		orderDir = "ASC"
	}

	// ── Fetch page with LEFT JOIN to first crypto payment ──
	dataQ := fmt.Sprintf(`
		SELECT i.id, i.number, i.status, i.expected_amount, i.received_amount,
			t.symbol, COALESCE(n.name, '') AS network, t.decimal_places,
			i.created_at,
			COALESCE(i.external_id, '') AS external_id,
			COALESCE(i.customer_email, '') AS customer_email,
			p.tx_hash, p.from_address,
			p.fee_fixed, p.fee_percent_bp, p.fee_min, p.fee_max,
			p.amount AS payment_amount,
			p.confirmed_at
		FROM doc_crypto_invoices i
		JOIN cat_tokens t ON t.id = i.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		LEFT JOIN LATERAL (
			SELECT tx_hash, from_address,
				fee_fixed, fee_percent_bp, fee_min, fee_max,
				amount, confirmed_at
			FROM doc_crypto_payments
			WHERE invoice_id = i.id AND _deleted_at IS NULL
			ORDER BY created_at DESC
			LIMIT 1
		) p ON true
		WHERE %s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d`,
		where, sortCol, orderDir, argIdx, argIdx+1,
	)
	args = append(args, limit, offset)

	rows, err := q.Query(ctx, dataQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("portal invoices: %w", err)
	}
	defer rows.Close()

	items := make([]dto.PortalInvoiceItem, 0, limit)
	for rows.Next() {
		var item dto.PortalInvoiceItem
		var iid, tokenID id.ID
		_ = tokenID // suppress unused, used only in scan
		var amount, received int64
		var createdAt time.Time
		var externalID, customerEmail string
		var txHash, fromAddress *string
		var feeFixed, feeMin, feeMax, paymentAmount *int64
		var feePercentBP *int
		var confirmedAt *time.Time

		if err := rows.Scan(
			&iid, &item.Number, &item.Status,
			&amount, &received,
			&item.Symbol, &item.Network, &item.DecimalPlaces,
			&createdAt,
			&externalID, &customerEmail,
			&txHash, &fromAddress,
			&feeFixed, &feePercentBP, &feeMin, &feeMax,
			&paymentAmount,
			&confirmedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("portal invoices scan: %w", err)
		}

		item.ID = iid.String()
		item.Amount = strconv.FormatInt(amount, 10)
		item.ReceivedAmount = strconv.FormatInt(received, 10)
		item.CreatedAt = createdAt.Format(time.RFC3339)
		item.ExternalID = externalID
		item.CustomerEmail = customerEmail

		// Payment details (if joined)
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

		// Calculate processing fee and net amount using the same formula as domain model:
		// fee = clamp(feeFixed + paymentAmount × feePercentBP / 10000, feeMin, feeMax)
		if paymentAmount != nil && feeFixed != nil && feePercentBP != nil {
			fee := calculateFee(*feeFixed, *feePercentBP, safeDeref(feeMin), safeDeref(feeMax), *paymentAmount)
			item.ProcessingFee = strconv.FormatInt(fee, 10)
			net := max(*paymentAmount-fee, 0)
			item.NetAmount = strconv.FormatInt(net, 10)
		}

		items = append(items, item)
	}
	return items, total, rows.Err()
}

// calculateFee replicates CryptoPayment.FeeAmount() logic in pure Go:
// clamp(fixed + amount * percentBP / 10000, minFee, maxFee).
func calculateFee(fixed int64, percentBP int, minFee, maxFee, amount int64) int64 {
	percentPart := amount * int64(percentBP) / 10000
	total := fixed + percentPart
	if minFee > 0 && total < minFee {
		total = minFee
	}
	if maxFee > 0 && total > maxFee {
		total = maxFee
	}
	if total < 0 {
		return 0
	}
	return total
}

// safeDeref returns the value behind a pointer, or 0 if nil.
func safeDeref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
