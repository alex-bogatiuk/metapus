package portal_repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/urlsafe"
	"metapus/internal/infrastructure/http/v1/dto"
)

// ── Conversion Funnel ──────────────────────────────────────────────────────

// GetFunnel returns invoice lifecycle conversion metrics.
func (r *DashboardRepo) GetFunnel(ctx context.Context, activeMerchantID *id.ID) (*dto.PortalFunnelResponse, error) {
	ids := scopeIDs(ctx, activeMerchantID)
	q := querier(ctx)

	const sql = `
		SELECT
			COUNT(*)                                                                         AS total,
			COUNT(*) FILTER (WHERE status IN ('partially_paid','paid','overpaid','confirmed')) AS received_any,
			COUNT(*) FILTER (WHERE status IN ('paid','overpaid','confirmed'))                  AS fully_paid,
			COUNT(*) FILTER (WHERE status = 'confirmed')                                      AS confirmed,
			COUNT(*) FILTER (WHERE status = 'expired')                                        AS expired
		FROM doc_crypto_invoices
		WHERE merchant_id = ANY($1) AND _deleted_at IS NULL`

	var resp dto.PortalFunnelResponse
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
func (r *DashboardRepo) ListPaymentLinks(ctx context.Context, activeMerchantID *id.ID, limit, offset int) ([]dto.PortalPaymentLinkItem, int, error) {
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
		return []dto.PortalPaymentLinkItem{}, 0, nil
	}

	// List with token info join
	rows, err := q.Query(ctx, `
		SELECT
			pl.id, pl.short_code, pl.amount::TEXT, pl.description,
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

	items := make([]dto.PortalPaymentLinkItem, 0, limit)
	for rows.Next() {
		var item dto.PortalPaymentLinkItem
		var createdAt time.Time
		if err := rows.Scan(
			&item.ID, &item.ShortCode, &item.Amount, &item.Description,
			&item.Reusable, &item.MaxUses, &item.CurrentUses, &item.Status, &item.TTLMinutes,
			&item.Symbol, &item.Network,
			&createdAt,
		); err != nil {
			return nil, 0, fmt.Errorf("portal payment links scan: %w", err)
		}
		item.PayURL = "/pay/link/" + item.ShortCode
		item.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, item)
	}
	return items, total, nil
}

// CreatePaymentLink inserts a new payment link for the given merchant.
func (r *DashboardRepo) CreatePaymentLink(
	ctx context.Context,
	merchantID id.ID,
	tokenID id.ID,
	amount *big.Int,
	description string,
	reusable bool,
	maxUses int,
	ttlMinutes int,
	createdBy *id.ID,
) (linkID id.ID, shortCode string, err error) {
	q := querier(ctx)

	shortCode, err = generateShortCode()
	if err != nil {
		return id.ID{}, "", fmt.Errorf("generate short code: %w", err)
	}

	err = q.QueryRow(ctx, `
		INSERT INTO doc_payment_links
			(merchant_id, token_id, amount, description, short_code, reusable, max_uses, ttl_minutes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`,
		merchantID, tokenID, amount.Int64(), description,
		shortCode, reusable, maxUses, ttlMinutes, createdBy,
	).Scan(&linkID)
	if err != nil {
		return id.ID{}, "", fmt.Errorf("insert payment link: %w", err)
	}
	return linkID, shortCode, nil
}

// IncrementPaymentLinkUses atomically increments current_uses and auto-disables
// the link when max_uses is reached. Returns (newCount, activated, err).
//
// Security: uses a single CTE with FOR UPDATE to prevent TOCTOU race condition.
// Without this, N concurrent requests could all read current_uses < max_uses
// and each increment past the limit.
//
// The CTE flow:
//  1. locked: SELECT ... FOR UPDATE — serializes concurrent access
//  2. guard:  WHERE ... current_uses < max_uses — rejects over-limit
//  3. UPDATE: increment + auto-disable in one statement
func (r *DashboardRepo) IncrementPaymentLinkUses(ctx context.Context, shortCode string) (int, bool, error) {
	q := querier(ctx)

	// Atomic CTE: lock → check limit → increment → auto-disable.
	// If the link is already at max_uses or disabled, zero rows are returned → pgx.ErrNoRows.
	const incrementSQL = `
		WITH locked AS (
			SELECT id, current_uses, max_uses, reusable
			FROM doc_payment_links
			WHERE short_code = $1
			  AND status = 'active'
			  AND _deleted_at IS NULL
			FOR UPDATE
		)
		UPDATE doc_payment_links pl
		SET
			current_uses = locked.current_uses + 1,
			updated_at   = NOW(),
			status       = CASE
				WHEN locked.reusable
				     AND locked.max_uses > 0
				     AND locked.current_uses + 1 >= locked.max_uses
				THEN 'disabled'
				ELSE pl.status
			END
		FROM locked
		WHERE pl.id = locked.id
		  AND (
		      NOT locked.reusable              -- single-use: allow exactly 1
		      OR locked.max_uses = 0           -- unlimited
		      OR locked.current_uses < locked.max_uses  -- under limit
		  )
		RETURNING pl.current_uses, pl.status = 'active'`

	var newCount int
	var stillActive bool
	err := q.QueryRow(ctx, incrementSQL, shortCode).Scan(&newCount, &stillActive)
	if err != nil {
		return 0, false, fmt.Errorf("increment payment link uses: %w", err)
	}

	return newCount, stillActive, nil
}

// GetPaymentLinkByCode returns a payment link by its short code.
func (r *DashboardRepo) GetPaymentLinkByCode(ctx context.Context, shortCode string) (*PaymentLinkRow, error) {
	q := querier(ctx)

	var row PaymentLinkRow
	err := q.QueryRow(ctx, `
		SELECT id, merchant_id, token_id, amount, description, reusable, max_uses,
		       current_uses, status, ttl_minutes
		FROM doc_payment_links
		WHERE short_code = $1 AND _deleted_at IS NULL
	`, shortCode).Scan(
		&row.ID, &row.MerchantID, &row.TokenID, &row.Amount, &row.Description,
		&row.Reusable, &row.MaxUses, &row.CurrentUses, &row.Status, &row.TTLMinutes,
	)
	if err != nil {
		return nil, fmt.Errorf("get payment link by code: %w", err)
	}
	return &row, nil
}

// PaymentLinkRow is a raw database row for a payment link.
type PaymentLinkRow struct {
	ID          id.ID
	MerchantID  id.ID
	TokenID     id.ID
	Amount      int64
	Description string
	Reusable    bool
	MaxUses     int
	CurrentUses int
	Status      string
	TTLMinutes  int
}

// ── Merchant Settings ──────────────────────────────────────────────────────

// GetMerchantSettings reads self-service settings for a merchant.
func (r *DashboardRepo) GetMerchantSettings(ctx context.Context, merchantID id.ID) (*dto.PortalSettingsResponse, error) {
	// Verify merchantID is in scope.
	scope := appctx.MustGetMerchantScope(ctx)
	inScope := slices.Contains(scope.MerchantIDs, merchantID)
	if !inScope {
		return nil, fmt.Errorf("merchant_id not in scope")
	}

	q := querier(ctx)
	var resp dto.PortalSettingsResponse
	err := q.QueryRow(ctx, `
		SELECT
			COALESCE(attributes->>'webhookUrl', '')   AS webhook_url,
			COALESCE((attributes->>'defaultTtlMinutes')::INT, 60) AS default_ttl_minutes
		FROM cat_merchants
		WHERE id = $1 AND _deleted_at IS NULL
	`, merchantID).Scan(&resp.WebhookURL, &resp.DefaultTTLMinutes)
	if err != nil {
		return nil, fmt.Errorf("portal get settings: %w", err)
	}
	return &resp, nil
}

// UpdateMerchantSettings updates self-service settings for a merchant.
func (r *DashboardRepo) UpdateMerchantSettings(ctx context.Context, merchantID id.ID, req dto.UpdatePortalSettingsRequest) error {
	// Verify scope
	scope := appctx.MustGetMerchantScope(ctx)
	inScope := slices.Contains(scope.MerchantIDs, merchantID)
	if !inScope {
		return fmt.Errorf("merchant_id not in scope")
	}

	// Validate webhook URL if provided
	if req.WebhookURL != nil && *req.WebhookURL != "" {
		if err := urlsafe.ValidatePublicURL(*req.WebhookURL, "webhookUrl"); err != nil {
			return err
		}
	}

	// Validate TTL if provided
	if req.DefaultTTLMinutes != nil {
		ttl := *req.DefaultTTLMinutes
		if ttl < 5 || ttl > 1440 {
			return fmt.Errorf("defaultTtlMinutes must be between 5 and 1440")
		}
	}

	q := querier(ctx)

	// Build JSONB merge patch: collect keys + positional args ($1 = merchantID).
	keys := make([]string, 0, 2)
	args := []any{merchantID}

	if req.WebhookURL != nil {
		keys = append(keys, "webhookUrl")
		args = append(args, *req.WebhookURL)
	}
	if req.DefaultTTLMinutes != nil {
		keys = append(keys, "defaultTtlMinutes")
		args = append(args, strconv.Itoa(*req.DefaultTTLMinutes))
	}

	if len(keys) == 0 {
		return nil // nothing to update
	}

	// Use jsonb || to merge each field, preserving other attributes.
	var query strings.Builder
	query.WriteString(`UPDATE cat_merchants SET updated_at = NOW(), attributes = attributes`)
	for i, key := range keys {
		query.WriteString(fmt.Sprintf(` || jsonb_build_object('%s', $%d)`, key, i+2))
	}
	query.WriteString(` WHERE id = $1 AND _deleted_at IS NULL`)

	_, err := q.Exec(ctx, query.String(), args...)
	if err != nil {
		return fmt.Errorf("portal update settings: %w", err)
	}
	return nil
}

// ── Helpers ────────────────────────────────────────────────────────────────

// generateShortCode creates a cryptographically random 24-char hex code.
// Uses 96 bits of entropy to prevent brute-force attacks against payment links.
func generateShortCode() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
