package portal_repo

import (
	"context"
	"fmt"
	"slices"
	"time"

	"metapus/internal/application/portalapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/merchant"
)

// ── Conversion Funnel ──────────────────────────────────────────────────────

// GetFunnel returns invoice lifecycle conversion metrics.
func (r *DashboardRepo) GetFunnel(ctx context.Context, activeMerchantID *id.ID) (*portalapp.PortalFunnelResponse, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	const sql = `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status IN ('partially_paid','paid','confirmed','completed','underpaid','reversed')) AS received_any,
			COUNT(*) FILTER (WHERE status IN ('paid','confirmed','completed','reversed')) AS fully_paid,
			COUNT(*) FILTER (WHERE status IN ('confirmed','completed','reversed')) AS confirmed,
			COUNT(*) FILTER (WHERE status IN ('expired','underpaid')) AS expired
		FROM doc_crypto_invoices
		WHERE merchant_id = ANY($1) AND _deleted_at IS NULL`

	var resp portalapp.PortalFunnelResponse
	err := q.QueryRow(ctx, sql, ids).Scan(
		&resp.Total, &resp.ReceivedAny, &resp.FullyPaid, &resp.Confirmed, &resp.Expired,
	)
	if err != nil {
		return nil, fmt.Errorf("portal funnel: %w", err)
	}
	return &resp, nil
}

// ── Payment Links ──────────────────────────────────────────────────────────

// ListPaymentLinks returns payment links for the scoped merchant.
func (r *DashboardRepo) ListPaymentLinks(ctx context.Context, activeMerchantID *id.ID, limit, offset int) ([]portalapp.PortalPaymentLinkItem, int, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	// Count
	var total int
	err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM doc_payment_links
		WHERE merchant_id = ANY($1) AND _deleted_at IS NULL
	`, ids).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("portal payment links count: %w", err)
	}

	if total == 0 {
		return []portalapp.PortalPaymentLinkItem{}, 0, nil
	}

	// List with token info join
	rows, err := q.Query(ctx, `
		SELECT
			pl.id, pl.public_ref, pl.amount::TEXT, pl.description,
			pl.reusable, pl.max_uses, pl.current_uses, pl.status, pl.ttl_minutes,
			COALESCE(t.code, '') AS symbol,
			COALESCE(n.name, '') AS network,
			pl.created_at
		FROM doc_payment_links pl
		LEFT JOIN cat_tokens t ON t.id = pl.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE pl.merchant_id = ANY($1) AND pl._deleted_at IS NULL
		ORDER BY pl.created_at DESC
		LIMIT $2 OFFSET $3
	`, ids, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("portal payment links list: %w", err)
	}
	defer rows.Close()

	items := make([]portalapp.PortalPaymentLinkItem, 0, limit)
	for rows.Next() {
		var item portalapp.PortalPaymentLinkItem
		var createdAt time.Time
		if err := rows.Scan(
			&item.ID, &item.PublicRef, &item.Amount, &item.Description,
			&item.Reusable, &item.MaxUses, &item.CurrentUses, &item.Status, &item.TTLMinutes,
			&item.Symbol, &item.Network,
			&createdAt,
		); err != nil {
			return nil, 0, fmt.Errorf("portal payment links scan: %w", err)
		}
		item.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, item)
	}
	return items, total, nil
}

// CreatePaymentLink inserts a new payment link for the given merchant.
func (r *DashboardRepo) CreatePaymentLink(
	ctx context.Context,
	linkID id.ID,
	merchantID id.ID,
	tokenID id.ID,
	amount types.RawAmount,
	description string,
	reusable bool,
	maxUses int,
	ttlMinutes int,
	createdBy *id.ID,
	publicRef string,
) error {
	q := querier(ctx)

	_, err := q.Exec(ctx, `
		INSERT INTO doc_payment_links
			(id, merchant_id, token_id, amount, description, public_ref, reusable, max_uses, ttl_minutes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		linkID, merchantID, tokenID, amount, description,
		publicRef, reusable, maxUses, ttlMinutes, createdBy,
	)
	if err != nil {
		return fmt.Errorf("insert payment link: %w", err)
	}
	return nil
}

// LockPaymentLinkForRedeem returns a payment link row and locks it until the
// surrounding transaction completes. Callers must run inside TxManager.
func (r *DashboardRepo) LockPaymentLinkForRedeem(ctx context.Context, publicRef string) (*PaymentLinkRow, error) {
	q := querier(ctx)

	var row PaymentLinkRow
	err := q.QueryRow(ctx, `
		SELECT id, public_ref, merchant_id, token_id, amount, description, reusable, max_uses,
		       current_uses, status, ttl_minutes
		FROM doc_payment_links
		WHERE public_ref = $1 AND _deleted_at IS NULL
		FOR UPDATE
	`, publicRef).Scan(
		&row.ID, &row.PublicRef, &row.MerchantID, &row.TokenID, &row.Amount, &row.Description,
		&row.Reusable, &row.MaxUses, &row.CurrentUses, &row.Status, &row.TTLMinutes,
	)
	if err != nil {
		return nil, fmt.Errorf("lock payment link for redeem: %w", err)
	}
	return &row, nil
}

// MarkPaymentLinkRedeemed increments usage after invoice creation succeeds and
// disables links whose configured usage limit has been reached.
func (r *DashboardRepo) MarkPaymentLinkRedeemed(ctx context.Context, linkID id.ID) (int, bool, error) {
	q := querier(ctx)

	const sql = `
		UPDATE doc_payment_links
		SET
			current_uses = current_uses + 1,
			updated_at   = NOW(),
			status       = CASE
				WHEN max_uses > 0 AND current_uses + 1 >= max_uses
				THEN 'disabled'
				ELSE status
			END
		WHERE id = $1
		  AND status = 'active'
		  AND _deleted_at IS NULL
		  AND (max_uses = 0 OR current_uses < max_uses)
		RETURNING current_uses, status = 'active'`

	var newCount int
	var stillActive bool
	if err := q.QueryRow(ctx, sql, linkID).Scan(&newCount, &stillActive); err != nil {
		return 0, false, fmt.Errorf("mark payment link redeemed: %w", err)
	}
	return newCount, stillActive, nil
}

// PaymentLinkRow is a raw database row for a payment link.
type PaymentLinkRow struct {
	ID          id.ID
	PublicRef   string
	MerchantID  id.ID
	TokenID     id.ID
	Amount      types.RawAmount
	Description string
	Reusable    bool
	MaxUses     int
	CurrentUses int
	Status      string
	TTLMinutes  int
}

// ── Merchant Settings ──────────────────────────────────────────────────────

// GetMerchantSettings reads self-service settings for a merchant.
func (r *DashboardRepo) GetMerchantSettings(ctx context.Context, merchantID id.ID) (*portalapp.PortalSettingsResponse, error) {
	// Verify merchantID is in scope.
	scope := merchant.MustGetPortalScope(ctx)
	inScope := slices.Contains(scope.MerchantIDs, merchantID)
	if !inScope {
		return nil, fmt.Errorf("merchant_id not in scope")
	}

	q := querier(ctx)
	var resp portalapp.PortalSettingsResponse
	err := q.QueryRow(ctx, `
		SELECT COALESCE((attributes->>'defaultTtlMinutes')::INT, 60) AS default_ttl_minutes,
		       COALESCE((attributes->>'singlePaymentPerInvoice')::BOOLEAN,FALSE)
		FROM cat_merchants
		WHERE id = $1 AND _deleted_at IS NULL
	`, merchantID).Scan(&resp.DefaultTTLMinutes, &resp.SinglePaymentPerInvoice)
	if err != nil {
		return nil, fmt.Errorf("portal get settings: %w", err)
	}
	return &resp, nil
}

// UpdateMerchantSettings updates self-service settings for a merchant.
func (r *DashboardRepo) UpdateMerchantSettings(ctx context.Context, merchantID id.ID, req portalapp.UpdatePortalSettingsRequest) error {
	// Verify scope
	scope := merchant.MustGetPortalScope(ctx)
	inScope := slices.Contains(scope.MerchantIDs, merchantID)
	if !inScope {
		return fmt.Errorf("merchant_id not in scope")
	}

	// Validate TTL if provided
	if req.DefaultTTLMinutes != nil {
		ttl := *req.DefaultTTLMinutes
		if ttl < 5 || ttl > 10080 {
			return fmt.Errorf("defaultTtlMinutes must be between 5 and 10080")
		}
	}

	// Include only supplied fields, keeping their native JSON scalar types.
	patch := make(map[string]any, 2)
	if req.DefaultTTLMinutes != nil {
		patch["defaultTtlMinutes"] = *req.DefaultTTLMinutes
	}
	if req.SinglePaymentPerInvoice != nil {
		patch["singlePaymentPerInvoice"] = *req.SinglePaymentPerInvoice
	}
	if len(patch) == 0 {
		return nil // nothing to update
	}

	// A typed JSONB parameter lets pgx encode the patch without ambiguous
	// polymorphic jsonb_build_object arguments. Merge preserves other attributes.
	_, err := querier(ctx).Exec(ctx, `
		UPDATE cat_merchants
		SET updated_at = NOW(), attributes = attributes || $2::jsonb
		WHERE id = $1 AND _deleted_at IS NULL
	`, merchantID, patch)
	if err != nil {
		return fmt.Errorf("portal update settings: %w", err)
	}
	return nil
}

// ── Helpers ────────────────────────────────────────────────────────────────
