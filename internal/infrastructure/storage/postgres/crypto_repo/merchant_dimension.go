package crypto_repo

import (
	"context"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// MerchantDimensionResolver dynamically resolves the "merchant" dimension
// for a user by querying the sys_merchant_users junction table.
//
// Used by SecurityContext middleware to inject merchant IDs into DataScope.
// This enables merchant-level RLS: a user associated with merchants M1, M2
// will only see invoices/payments/withdrawals for M1 and M2.
type MerchantDimensionResolver struct{}

// NewMerchantDimensionResolver creates a new resolver.
func NewMerchantDimensionResolver() *MerchantDimensionResolver {
	return &MerchantDimensionResolver{}
}

// DimensionName implements DimensionResolver.
func (r *MerchantDimensionResolver) DimensionName() string { return "merchant" }

// Resolve implements DimensionResolver.
// Returns merchant IDs associated with the user, or nil if user has no merchant associations.
func (r *MerchantDimensionResolver) Resolve(ctx context.Context, userID id.ID) ([]string, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	const query = `
		SELECT merchant_id::text FROM sys_merchant_users 
		WHERE user_id = $1 AND is_active = true
	`

	var merchantIDs []string
	if err := pgxscan.Select(ctx, querier, &merchantIDs, query, userID); err != nil {
		return nil, fmt.Errorf("resolve merchant dimension for user %s: %w", userID, err)
	}

	if len(merchantIDs) == 0 {
		return nil, nil // No merchant associations → dimension not applied
	}

	return merchantIDs, nil
}
