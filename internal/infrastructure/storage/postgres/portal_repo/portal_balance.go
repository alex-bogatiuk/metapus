package portal_repo

import (
	"context"
	"fmt"
	"strconv"

	"metapus/internal/core/id"
)

// PendingAmount holds the sum of received_amount for pending (unconfirmed) invoices per token.
type PendingAmount struct {
	TokenID id.ID
	Amount  int64 // minor units
}

// GetPendingAmounts returns the sum of received_amount for invoices in 'paid' status
// (transaction detected but not yet confirmed on-chain) per token.
// Only 'paid' status is considered pending вЂ” 'partially_paid' is excluded because
// the merchant hasn't received the full amount yet.
func (r *DashboardRepo) GetPendingAmounts(ctx context.Context, merchantIDs []id.ID) (map[string]int64, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT i.token_id::text, COALESCE(SUM(i.received_amount), 0) AS pending_total
		FROM doc_crypto_invoices i
		WHERE i.merchant_id = ANY($1)
		  AND i.status = 'paid'
		  AND i._deleted_at IS NULL
		GROUP BY i.token_id
	`

	rows, err := q.Query(ctx, sqlQuery, merchantIDs)
	if err != nil {
		return nil, fmt.Errorf("get pending amounts: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64, 8)
	for rows.Next() {
		var tokenID string
		var amount int64
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
	ID            id.ID
	NetworkID     id.ID
	Symbol        string
	Network       string
	DecimalPlaces int
}

// FormatMinorUnits converts an int64 minor units amount to a human-readable string.
// E.g. 1500000 with 6 decimals в†’ "1.500000"
func FormatMinorUnits(amount int64, decimalPlaces int) string {
	if decimalPlaces == 0 {
		return strconv.FormatInt(amount, 10)
	}

	str := strconv.FormatInt(amount, 10)
	if amount < 0 {
		str = str[1:] // handle negative
	}

	// Pad to at least decimalPlaces+1 digits
	for len(str) <= decimalPlaces {
		str = "0" + str
	}

	intPart := str[:len(str)-decimalPlaces]
	fracPart := str[len(str)-decimalPlaces:]

	result := intPart + "." + fracPart
	if amount < 0 {
		result = "-" + result
	}
	return result
}

// GetTokenMeta returns token scale and network data by ID.
// Used by portal handlers to validate cross-network operations and convert amounts.
func (r *DashboardRepo) GetTokenMeta(ctx context.Context, tokenID id.ID) (TokenMeta, error) {
	q := querier(ctx)

	var meta TokenMeta
	err := q.QueryRow(ctx,
		`SELECT t.id, t.network_id, t.symbol, COALESCE(n.name, '') AS network, t.decimal_places
		 FROM cat_tokens t
		 LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		 WHERE t.id = $1 AND t.deletion_mark = FALSE`,
		tokenID,
	).Scan(&meta.ID, &meta.NetworkID, &meta.Symbol, &meta.Network, &meta.DecimalPlaces)
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
