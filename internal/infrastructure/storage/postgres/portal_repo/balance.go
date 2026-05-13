package portal_repo

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

// GetMerchantBalances implements crypto.BalanceQueryRepo.
// Joins reg_crypto_merchant_balance_balances with cat_tokens and cat_currencies
// to produce fully enriched BalanceRow for the BalanceCalculator.
func (r *DashboardRepo) GetMerchantBalances(ctx context.Context, merchantIDs []id.ID) ([]crypto.BalanceRow, error) {
	q := querier(ctx)

	const sql = `
		SELECT b.merchant_id,
		       b.token_id,
		       t.symbol,
		       t.decimal_places,
		       t.currency_id,
		       COALESCE(c.iso_code, '') AS iso_code,
		       b.amount
		FROM reg_crypto_merchant_balance_balances b
		JOIN cat_tokens t ON t.id = b.token_id
		LEFT JOIN cat_currencies c ON c.id = t.currency_id
		WHERE b.merchant_id = ANY($1)
		  AND b.amount != 0
		ORDER BY b.amount DESC
	`

	rows, err := q.Query(ctx, sql, merchantIDs)
	if err != nil {
		return nil, fmt.Errorf("query merchant balances: %w", err)
	}
	defer rows.Close()

	var result []crypto.BalanceRow
	for rows.Next() {
		var row crypto.BalanceRow
		var rawAmount int64
		if err := rows.Scan(
			&row.MerchantID,
			&row.TokenID,
			&row.TokenSymbol,
			&row.DecimalPlaces,
			&row.CurrencyID,
			&row.CurrencyCode,
			&rawAmount,
		); err != nil {
			return nil, fmt.Errorf("scan merchant balance: %w", err)
		}
		row.RawAmount = types.NewCryptoAmountFromInt64(rawAmount)
		result = append(result, row)
	}

	return result, rows.Err()
}

// Compile-time interface check.
var _ crypto.BalanceQueryRepo = (*DashboardRepo)(nil)
