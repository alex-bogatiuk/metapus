package portal_repo

import (
	"context"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// RateSourceResolver resolves rate source code to UUID.
// Implements crypto.RateSourceResolver.
type RateSourceResolver struct{}

// NewRateSourceResolver creates a new resolver.
func NewRateSourceResolver() *RateSourceResolver {
	return &RateSourceResolver{}
}

// ResolveRateSourceID resolves a rate source code (e.g. "coingecko") to its UUID.
func (r *RateSourceResolver) ResolveRateSourceID(ctx context.Context, code string) (id.ID, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	const query = `
		SELECT id FROM cat_rate_sources
		WHERE code = $1 AND _deleted_at IS NULL AND is_active = TRUE
		LIMIT 1
	`

	var rateSourceID id.ID
	if err := pgxscan.Get(ctx, querier, &rateSourceID, query, code); err != nil {
		return id.ID{}, fmt.Errorf("resolve rate source '%s': %w", code, err)
	}

	return rateSourceID, nil
}
