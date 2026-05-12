// Package portal_repo provides database access for the merchant portal.
package portal_repo

import (
	"context"
	"fmt"
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

// scopeIDs returns merchant IDs from context. If activeMerchantID is provided
// and belongs to the scope, returns only that one ID; otherwise returns all.
func scopeIDs(ctx context.Context, activeMerchantID *id.ID) []id.ID {
	scope := appctx.MustGetMerchantScope(ctx)
	if activeMerchantID != nil {
		for _, sid := range scope.MerchantIDs {
			if sid == *activeMerchantID {
				return []id.ID{*activeMerchantID}
			}
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
		SELECT t.symbol, COALESCE(n.name, '') AS network, t.decimal_places,
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
		GROUP BY t.id, t.symbol, n.name, t.decimal_places
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
			&item.Symbol, &item.Network, &item.DecimalPlaces,
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

// ListInvoices returns a page of invoices for the scoped merchants.
func (r *DashboardRepo) ListInvoices(ctx context.Context, activeMerchantID *id.ID, status string, limit, offset int) ([]dto.PortalInvoiceItem, int, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	// Count total
	countQ := `
		SELECT COUNT(*)
		FROM doc_crypto_invoices
		WHERE merchant_id = ANY($1)
			AND _deleted_at IS NULL`
	countArgs := []any{ids}
	if status != "" {
		countQ += ` AND status = $2`
		countArgs = append(countArgs, status)
	}

	var total int
	if err := q.QueryRow(ctx, countQ, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("portal invoices count: %w", err)
	}

	// Fetch page
	dataQ := `
		SELECT i.id, i.number, i.status, i.expected_amount, i.received_amount,
			t.symbol, COALESCE(n.name, '') AS network, t.decimal_places,
			i.created_at
		FROM doc_crypto_invoices i
		JOIN cat_tokens t ON t.id = i.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE i.merchant_id = ANY($1)
			AND i._deleted_at IS NULL`
	dataArgs := []any{ids}
	argIdx := 2
	if status != "" {
		dataQ += fmt.Sprintf(` AND i.status = $%d`, argIdx)
		dataArgs = append(dataArgs, status)
		argIdx++
	}
	dataQ += fmt.Sprintf(` ORDER BY i.created_at DESC LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	dataArgs = append(dataArgs, limit, offset)

	rows, err := q.Query(ctx, dataQ, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("portal invoices: %w", err)
	}
	defer rows.Close()

	items := make([]dto.PortalInvoiceItem, 0, limit)
	for rows.Next() {
		var item dto.PortalInvoiceItem
		var iid id.ID
		var amount, received int64
		var createdAt time.Time
		if err := rows.Scan(
			&iid, &item.Number, &item.Status,
			&amount, &received,
			&item.Symbol, &item.Network, &item.DecimalPlaces,
			&createdAt,
		); err != nil {
			return nil, 0, fmt.Errorf("portal invoices scan: %w", err)
		}
		item.ID = iid.String()
		item.Amount = strconv.FormatInt(amount, 10)
		item.ReceivedAmount = strconv.FormatInt(received, 10)
		item.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, item)
	}
	return items, total, rows.Err()
}
