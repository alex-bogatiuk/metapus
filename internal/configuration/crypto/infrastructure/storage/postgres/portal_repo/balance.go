package portal_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// GetMerchantBalances implements crypto.BalanceQueryRepo.
// Joins reg_crypto_merchant_balance_balances with cat_tokens and takes the
// currency codes from the platform to produce fully enriched BalanceRow for
// the BalanceCalculator.
func (r *DashboardRepo) GetMerchantBalances(ctx context.Context, merchantIDs []id.ID) ([]crypto.BalanceRow, error) {
	q := querier(ctx)

	const sql = `
		SELECT b.merchant_id,
		       b.token_id,
	       t.symbol,
	       t.network_id,
	       COALESCE(n.name, ''),
		       t.decimal_places,
		       t.currency_id,
		       b.amount
		FROM (
            SELECT merchant_id,token_id,amount FROM reg_crypto_merchant_balance_balances WHERE merchant_id=ANY($1)
            UNION ALL
            SELECT DISTINCT p.merchant_id,p.token_id,0::numeric FROM doc_crypto_payments p
            WHERE p.merchant_id=ANY($1) AND p.status IN ('detected','confirming','confirmed')
              AND p.canonicality='canonical' AND p.posted=FALSE
              AND p._deleted_at IS NULL AND p.deletion_mark=FALSE
              AND NOT EXISTS (SELECT 1 FROM reg_crypto_merchant_balance_balances b WHERE b.merchant_id=p.merchant_id AND b.token_id=p.token_id)
        ) b
		JOIN cat_tokens t ON t.id = b.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE b.merchant_id = ANY($1)
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
		if err := rows.Scan(
			&row.MerchantID,
			&row.TokenID,
			&row.TokenSymbol,
			&row.NetworkID,
			&row.Network,
			&row.DecimalPlaces,
			&row.CurrencyID,
			&row.RawAmount,
		); err != nil {
			return nil, fmt.Errorf("scan merchant balance: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	currencies := make([]id.ID, 0, len(result))
	for _, row := range result {
		if row.CurrencyID != nil {
			currencies = append(currencies, *row.CurrencyID)
		}
	}
	codes, err := postgres.CurrencyCodes(ctx, currencies)
	if err != nil {
		return nil, fmt.Errorf("read merchant balance currencies: %w", err)
	}
	for i := range result {
		if result[i].CurrencyID != nil {
			result[i].CurrencyCode = codes[*result[i].CurrencyID]
		}
	}
	return result, nil
}

// Compile-time interface check.
var _ crypto.BalanceQueryRepo = (*DashboardRepo)(nil)
