package portal_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/portalapp"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

// ── Webhook Secret ────────────────────────────────────────────────────

// ── Fee Schedule (read-only for portal) ───────────────────────────────

// GetEffectiveFees returns the effective fee schedule for a merchant.
// Merges merchant-specific overrides with global defaults using COALESCE.
// Only returns "processing" and "withdrawal" directions (merchant-visible).
func (r *DashboardRepo) GetEffectiveFees(ctx context.Context, merchantID id.ID) ([]portalapp.PortalFeeItem, error) {
	q := querier(ctx)

	const sqlQuery = `
		WITH merchant_fees AS (
			SELECT token_id, direction, fixed_fee, percent_bp, min_fee, max_fee
			FROM reg_fee_schedule
			WHERE merchant_id = $1
		),
		global_fees AS (
			SELECT token_id, direction, fixed_fee, percent_bp, min_fee, max_fee
			FROM reg_fee_schedule
			WHERE merchant_id IS NULL
		),
		effective AS (
			SELECT
				COALESCE(mf.token_id, gf.token_id) AS token_id,
				COALESCE(mf.direction, gf.direction) AS direction,
				COALESCE(mf.fixed_fee, gf.fixed_fee, 0) AS fixed_fee,
				COALESCE(mf.percent_bp, gf.percent_bp, 0) AS percent_bp,
				COALESCE(mf.min_fee, gf.min_fee, 0) AS min_fee,
				COALESCE(mf.max_fee, gf.max_fee, 0) AS max_fee,
				(mf.token_id IS NOT NULL) AS is_custom
			FROM global_fees gf
			FULL OUTER JOIN merchant_fees mf
				ON mf.token_id = gf.token_id AND mf.direction = gf.direction
		)
		SELECT
			t.symbol,
			COALESCE(n.name, '') AS network,
			e.direction,
			e.fixed_fee::TEXT,
			e.percent_bp,
			e.min_fee::TEXT,
			e.max_fee::TEXT,
			t.decimal_places,
			e.is_custom
		FROM effective e
		JOIN cat_tokens t ON t.id = e.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE e.direction IN ('processing', 'withdrawal')
		ORDER BY t.symbol, e.direction
	`

	rows, err := q.Query(ctx, sqlQuery, merchantID)
	if err != nil {
		return nil, fmt.Errorf("get effective fees: %w", err)
	}
	defer rows.Close()

	items := make([]portalapp.PortalFeeItem, 0, 20)
	for rows.Next() {
		var item portalapp.PortalFeeItem
		if err := rows.Scan(
			&item.TokenSymbol,
			&item.Network,
			&item.Direction,
			&item.FixedFee,
			&item.PercentBP,
			&item.MinFee,
			&item.MaxFee,
			&item.DecimalPlaces,
			&item.IsCustom,
		); err != nil {
			return nil, fmt.Errorf("scan fee item: %w", err)
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
