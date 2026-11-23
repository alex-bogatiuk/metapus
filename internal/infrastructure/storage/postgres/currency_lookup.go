package postgres

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
)

// CurrencyCodes returns the ISO codes of the currencies among currencyIDs,
// such as the currencies a configuration's assets are priced in. A currency
// marked for deletion keeps its code, so rows that still reference it show
// it; a missing currency has none.
func CurrencyCodes(ctx context.Context, currencyIDs []id.ID) (map[id.ID]string, error) {
	codes := make(map[id.ID]string, len(currencyIDs))
	if len(currencyIDs) == 0 {
		return codes, nil
	}
	rows, err := MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx,
		`SELECT id, iso_code FROM cat_currencies WHERE id = ANY($1)`, currencyIDs)
	if err != nil {
		return nil, fmt.Errorf("read currency codes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var currencyID id.ID
		var code string
		if err := rows.Scan(&currencyID, &code); err != nil {
			return nil, fmt.Errorf("scan currency code: %w", err)
		}
		codes[currencyID] = code
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read currency codes: %w", err)
	}
	return codes, nil
}
