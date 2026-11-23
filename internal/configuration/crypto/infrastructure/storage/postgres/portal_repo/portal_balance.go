package portal_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// PendingAmount holds current receipts awaiting posting per token.
type PendingAmount struct {
	TokenID id.ID
	Amount  types.RawAmount // raw token units
}

// GetPendingAmounts includes partial payments and excludes already posted,
// noncanonical and failed receipts. Invoice lifecycle status is not a funds bucket.
func (r *DashboardRepo) GetPendingAmounts(ctx context.Context, merchantIDs []id.ID) (map[string]types.RawAmount, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT p.token_id::text, COALESCE(SUM(p.amount), 0) AS pending_total
		FROM doc_crypto_payments p
		WHERE p.merchant_id = ANY($1)
		  AND p.status IN ('detected','confirming','confirmed')
		  AND p.canonicality = 'canonical' AND p.posted = FALSE
		  AND p._deleted_at IS NULL AND p.deletion_mark = FALSE
		GROUP BY p.token_id
	`

	rows, err := q.Query(ctx, sqlQuery, merchantIDs)
	if err != nil {
		return nil, fmt.Errorf("get pending amounts: %w", err)
	}
	defer rows.Close()

	result := make(map[string]types.RawAmount, 8)
	for rows.Next() {
		var tokenID string
		var amount types.RawAmount
		if err := rows.Scan(&tokenID, &amount); err != nil {
			return nil, fmt.Errorf("scan pending amount: %w", err)
		}
		result[tokenID] = amount
	}

	return result, rows.Err()
}

// GetAvailableTokens returns all active tokens for a set of merchants, even those
// with zero confirmed balance (needed for invoice creation token selector).
func (r *DashboardRepo) GetAvailableTokens(ctx context.Context, merchantIDs []id.ID) ([]AvailableToken, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT DISTINCT t.id::text, t.symbol, COALESCE(n.name, '') AS network, t.decimal_places
		FROM doc_crypto_invoices i
		JOIN cat_tokens t ON t.id = i.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE i.merchant_id = ANY($1) AND i._deleted_at IS NULL
		UNION
		SELECT t.id::text, t.symbol, COALESCE(n.name, '') AS network, t.decimal_places
		FROM cat_tokens t
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE t.deletion_mark = FALSE
		ORDER BY symbol
	`

	rows, err := q.Query(ctx, sqlQuery, merchantIDs)
	if err != nil {
		return nil, fmt.Errorf("get available tokens: %w", err)
	}
	defer rows.Close()

	items := make([]AvailableToken, 0, 16)
	for rows.Next() {
		var t AvailableToken
		if err := rows.Scan(&t.TokenID, &t.Symbol, &t.Network, &t.DecimalPlaces); err != nil {
			return nil, fmt.Errorf("scan available token: %w", err)
		}
		items = append(items, t)
	}

	return items, rows.Err()
}

// AvailableToken is a lightweight token entry for dropdowns.
type AvailableToken struct {
	TokenID       string
	Symbol        string
	Network       string
	DecimalPlaces int
}

// TokenMeta contains token metadata needed by portal write handlers.
type TokenMeta struct {
	ID              id.ID
	NetworkID       id.ID
	NetworkCode     string
	Symbol          string
	Network         string
	DecimalPlaces   int
	ContractAddress string
	TokenStandard   string
}

// FormatMinorUnits converts raw token units to a human-readable decimal string.
// E.g. 1500000 with 6 decimals в†’ "1.500000"
func FormatMinorUnits(amount types.RawAmount, decimalPlaces int) string {
	return amount.ToDecimal(decimalPlaces).StringFixed(int32(decimalPlaces))
}

// GetTokenMeta returns token scale and network data by ID.
// Used by portal handlers to validate cross-network operations and convert amounts.
func (r *DashboardRepo) GetTokenMeta(ctx context.Context, tokenID id.ID) (TokenMeta, error) {
	q := querier(ctx)

	var meta TokenMeta
	err := q.QueryRow(ctx,
		`SELECT t.id, t.network_id, COALESCE(n.code, '') AS network_code,
		        t.symbol, COALESCE(n.name, '') AS network, t.decimal_places,
		        COALESCE(t.contract_address, '') AS contract_address,
		        t.token_standard
		 FROM cat_tokens t
		 LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		 WHERE t.id = $1 AND t.deletion_mark = FALSE`,
		tokenID,
	).Scan(&meta.ID, &meta.NetworkID, &meta.NetworkCode, &meta.Symbol, &meta.Network, &meta.DecimalPlaces, &meta.ContractAddress, &meta.TokenStandard)
	if err != nil {
		return TokenMeta{}, fmt.Errorf("get token meta: %w", err)
	}
	return meta, nil
}

// GetTokenDecimalPlaces returns the decimal_places for a token by ID.
// Used by callers that only need amount conversion.
func (r *DashboardRepo) GetTokenDecimalPlaces(ctx context.Context, tokenID id.ID) (int, error) {
	meta, err := r.GetTokenMeta(ctx, tokenID)
	if err != nil {
		return 0, err
	}
	return meta.DecimalPlaces, nil
}
