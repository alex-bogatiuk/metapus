// Package portal_repo provides database access for the merchant portal.
package portal_repo

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"metapus/internal/application/portalapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/crypto"
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

// ScopeIDs returns merchant IDs from context. If activeMerchantID is provided,
// returns only that one ID when it belongs to the scope and no IDs otherwise.
// If activeMerchantID is nil, returns all scoped merchant IDs.
func (r *DashboardRepo) ScopeIDs(ctx context.Context, activeMerchantID *id.ID) []id.ID {
	return scopeIDs(ctx, activeMerchantID)
}

// scopeIDs returns merchant IDs from context. If activeMerchantID is provided,
// returns only that one ID when it belongs to the scope and no IDs otherwise.
// If activeMerchantID is nil, returns all scoped merchant IDs.
func scopeIDs(ctx context.Context, activeMerchantID *id.ID) []id.ID {
	scope := merchant.MustGetPortalScope(ctx)
	if activeMerchantID != nil {
		if slices.Contains(scope.MerchantIDs, *activeMerchantID) {
			return []id.ID{*activeMerchantID}
		}
		return []id.ID{}
	}
	// Defensive copy (invariant §2.13)
	ids := make([]id.ID, len(scope.MerchantIDs))
	copy(ids, scope.MerchantIDs)
	return ids
}

// GetSummary keeps invoice counts and posted receipts in their own asset units.
func (r *DashboardRepo) GetSummary(ctx context.Context, activeMerchantID *id.ID) (*portalapp.PortalSummaryResponse, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)
	result := &portalapp.PortalSummaryResponse{ReceiptTotals: []portalapp.PortalReceiptTotal{}}
	err := q.QueryRow(ctx, `SELECT COUNT(*),
 COUNT(*) FILTER(WHERE status IN ('confirmed','completed')),
 COUNT(*) FILTER(WHERE status IN ('created','partially_paid','paid'))
 FROM doc_crypto_invoices WHERE merchant_id=ANY($1) AND _deleted_at IS NULL`, ids).
		Scan(&result.TotalInvoices, &result.PaidInvoices, &result.PendingInvoices)
	if err != nil {
		return nil, fmt.Errorf("portal invoice summary: %w", err)
	}
	rows, err := q.Query(ctx, `SELECT p.token_id,t.network_id,t.symbol,t.decimal_places,
 COALESCE(SUM(p.amount),0),
 COALESCE(SUM(p.amount) FILTER(WHERE COALESCE(p.confirmed_at,p.date)>=NOW()-INTERVAL '24 hours'),0),
 COALESCE(SUM(p.amount) FILTER(WHERE COALESCE(p.confirmed_at,p.date)>=NOW()-INTERVAL '48 hours' AND COALESCE(p.confirmed_at,p.date)<NOW()-INTERVAL '24 hours'),0)
 FROM doc_crypto_payments p JOIN cat_tokens t ON t.id=p.token_id
 WHERE p.merchant_id=ANY($1) AND p.posted AND p.canonicality='canonical'
 AND p.status IN ('confirmed','settled') AND p._deleted_at IS NULL AND NOT p.deletion_mark
 GROUP BY p.token_id,t.network_id,t.symbol,t.decimal_places ORDER BY p.token_id`, ids)
	if err != nil {
		return nil, fmt.Errorf("portal receipt totals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item portalapp.PortalReceiptTotal
		var tokenID, networkID id.ID
		var total, current, previous types.RawAmount
		if err := rows.Scan(&tokenID, &networkID, &item.Symbol, &item.DecimalPlaces, &total, &current, &previous); err != nil {
			return nil, fmt.Errorf("portal receipt total: %w", err)
		}
		item.TokenID, item.NetworkID = tokenID.String(), networkID.String()
		item.TotalMinor, item.Received24hMinor = total.String(), current.String()
		item.Change24hPct = portalapp.ReceiptChangePercent(current, previous)
		result.ReceiptTotals = append(result.ReceiptTotals, item)
	}
	return result, rows.Err()
}

// GetCurrencies returns currency breakdown for the scoped merchants.
func (r *DashboardRepo) GetCurrencies(ctx context.Context, activeMerchantID *id.ID) ([]portalapp.PortalCurrencyItem, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	const sql = `
		SELECT t.id::text AS token_id, t.symbol,
			t.network_id::text AS network_id, COALESCE(n.name, '') AS network,
			t.decimal_places,
			COUNT(i.id) AS cnt,
			COALESCE(SUM(i.received_amount), 0) AS total_minor,
			COALESCE(
				ROUND(COUNT(i.id) * 100.0 /
					NULLIF(SUM(COUNT(i.id)) OVER (), 0), 2),
				0
			) AS invoice_share_pct
		FROM cat_tokens t
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		LEFT JOIN doc_crypto_invoices i
			ON i.token_id = t.id
			AND i.merchant_id = ANY($1)
			AND i.status IN ('confirmed','completed')
			AND i._deleted_at IS NULL
		WHERE t.is_active = TRUE
			AND t.deletion_mark = FALSE
			AND t._deleted_at IS NULL
			AND n.is_active = TRUE
			AND n.deletion_mark = FALSE
			AND n._deleted_at IS NULL
		GROUP BY t.id, t.symbol, t.network_id, n.name, t.decimal_places
		ORDER BY cnt DESC, t.symbol, n.name`

	rows, err := q.Query(ctx, sql, ids)
	if err != nil {
		return nil, fmt.Errorf("portal currencies: %w", err)
	}
	defer rows.Close()

	items := make([]portalapp.PortalCurrencyItem, 0, 8)
	for rows.Next() {
		var item portalapp.PortalCurrencyItem
		var totalMinor types.RawAmount
		var sharePct float64
		if err := rows.Scan(
			&item.TokenID, &item.Symbol, &item.NetworkID, &item.Network, &item.DecimalPlaces,
			&item.Count, &totalMinor, &sharePct,
		); err != nil {
			return nil, fmt.Errorf("portal currencies scan: %w", err)
		}
		item.TotalMinor = totalMinor.String()
		item.InvoiceSharePct = strconv.FormatFloat(sharePct, 'f', 2, 64)
		items = append(items, item)
	}

	return items, rows.Err()
}

// GetChart returns confirmed gross receipts per token and UTC confirmation day.
// Invoice status/creation time is not a receipt timestamp: completed and underpaid
// invoices may both contain posted payments. Reorged/noncanonical receipts are excluded.
func (r *DashboardRepo) GetChart(ctx context.Context, period string, activeMerchantID *id.ID) ([]portalapp.PortalChartPoint, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	days := 30
	switch period {
	case "7d":
		days = 7
	case "90d":
		days = 90
	}

	const query = `
		SELECT (COALESCE(p.confirmed_at, p.date) AT TIME ZONE 'UTC')::date AS day,
			SUM(p.amount), p.token_id, t.symbol, COALESCE(n.name, ''), t.decimal_places
		FROM doc_crypto_payments p
		JOIN cat_tokens t ON t.id = p.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE p.merchant_id = ANY($1)
			AND p.status IN ('confirmed', 'settled')
			AND p.canonicality = 'canonical' AND p.posted = TRUE
			AND p._deleted_at IS NULL AND p.deletion_mark = FALSE
			AND COALESCE(p.confirmed_at, p.date) >=
				((NOW() AT TIME ZONE 'UTC')::date - ($2::int - 1))::timestamp AT TIME ZONE 'UTC'
			AND COALESCE(p.confirmed_at, p.date) <
				((NOW() AT TIME ZONE 'UTC')::date + 1)::timestamp AT TIME ZONE 'UTC'
		GROUP BY day, p.token_id, t.symbol, n.name, t.decimal_places
		ORDER BY day, t.symbol, n.name, p.token_id`

	rows, err := q.Query(ctx, query, ids, days)
	if err != nil {
		return nil, fmt.Errorf("portal chart: %w", err)
	}
	defer rows.Close()

	points := make([]portalapp.PortalChartPoint, 0, 30)
	for rows.Next() {
		var day time.Time
		var deposits types.RawAmount
		var tokenID id.ID
		var symbol, network string
		var decimalPlaces int
		if err := rows.Scan(&day, &deposits, &tokenID, &symbol, &network, &decimalPlaces); err != nil {
			return nil, fmt.Errorf("portal chart scan: %w", err)
		}
		points = append(points, portalapp.PortalChartPoint{
			Day:           day.Format("2006-01-02"),
			Deposits:      deposits.String(),
			TokenID:       tokenID.String(),
			Symbol:        symbol,
			Network:       network,
			DecimalPlaces: decimalPlaces,
		})
	}

	return points, rows.Err()
}

// ListMerchants returns the merchants available in the scope.
func (r *DashboardRepo) ListMerchants(ctx context.Context) ([]portalapp.PortalMerchantItem, error) {
	scope := merchant.MustGetPortalScope(ctx)
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

	items := make([]portalapp.PortalMerchantItem, 0, len(scope.MerchantIDs))
	for rows.Next() {
		var item portalapp.PortalMerchantItem
		var mid id.ID
		if err := rows.Scan(&mid, &item.Name, &item.Code); err != nil {
			return nil, fmt.Errorf("portal merchants scan: %w", err)
		}
		item.ID = mid.String()
		item.Role = int(scope.Roles[mid])
		items = append(items, item)
	}
	return items, rows.Err()
}

// InvoiceFilter contains filter parameters for the invoice list.
type InvoiceFilter struct {
	Status   string // invoice status filter
	Search   string // free-text search (number, externalId, orderId, customerId)
	TokenID  string // token UUID filter
	DateFrom string // ISO 8601 date (inclusive)
	DateTo   string // ISO 8601 date (inclusive, end of day)
	Sort     string // column to sort by (validated in handler)
	Order    string // "asc" or "desc" (validated in handler)
}

// ListInvoices returns a page of invoices for the scoped merchants.
// Payment totals are calculated across every current canonical receipt.
func (r *DashboardRepo) ListInvoices(ctx context.Context, activeMerchantID *id.ID, filter InvoiceFilter, limit, offset int) ([]portalapp.PortalInvoiceItem, int, error) {
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
		where += fmt.Sprintf(` AND (i.number ILIKE $%d OR i.external_id ILIKE $%d OR i.order_id ILIKE $%d OR i.customer_id ILIKE $%d)`, argIdx, argIdx, argIdx, argIdx)
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

	// ── Fetch page of invoices ──
	dataQ := fmt.Sprintf(`
		SELECT i.id, i.number, i.status, i.expected_amount, i.received_amount,
			t.symbol, COALESCE(n.name, '') AS network,
			COALESCE(n.explorer_url, '') AS explorer_url, t.decimal_places,
			i.created_at,
			COALESCE(i.external_id, '') AS external_id,
			COALESCE(i.order_id, '') AS order_id,
			COALESCE(i.customer_id, '') AS customer_id,
			COALESCE(i.invoice_kind, 'payment') AS invoice_kind
		FROM doc_crypto_invoices i
		JOIN cat_tokens t ON t.id = i.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
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

	items := make([]portalapp.PortalInvoiceItem, 0, limit)
	invoiceIDs := make([]id.ID, 0, limit)
	for rows.Next() {
		var item portalapp.PortalInvoiceItem
		var iid id.ID
		var amount, received types.RawAmount
		var createdAt time.Time
		var externalID, orderID, customerID, invoiceKind string

		if err := rows.Scan(
			&iid, &item.Number, &item.Status,
			&amount, &received,
			&item.Symbol, &item.Network, &item.ExplorerURL, &item.DecimalPlaces,
			&createdAt,
			&externalID, &orderID, &customerID, &invoiceKind,
		); err != nil {
			return nil, 0, fmt.Errorf("portal invoices scan: %w", err)
		}

		item.ID = iid.String()
		item.Amount = amount.String()
		item.ReceivedAmount = received.String()
		item.CreatedAt = createdAt.Format(time.RFC3339)
		item.ExternalID = externalID
		item.OrderID = orderID
		item.CustomerID = customerID
		item.InvoiceKind = invoiceKind

		invoiceIDs = append(invoiceIDs, iid)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	payments, err := r.loadInvoicePayments(ctx, invoiceIDs, false)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		applyPaymentSummary(&items[i], payments[items[i].ID])
	}
	return items, total, nil
}

// calculateFee replicates CryptoPayment.FeeAmount() logic in pure Go:
// clamp(fixed + amount * percentBP / 10000, minFee, maxFee).
func calculateFee(fixed types.RawAmount, percentBP int, minFee, maxFee, amount types.RawAmount) types.RawAmount {
	return (crypto.EffectiveFee{
		FixedFee: fixed, PercentBP: percentBP, MinFee: minFee, MaxFee: maxFee,
	}).Calculate(amount)
}
