package crypto_worker

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

// merchantCommissionAdapter implements crypto.CommissionLookup
// by querying cat_merchants for the merchant's commission_rate.
type merchantCommissionAdapter struct {
	repo *catalog_repo.MerchantRepo
}

// newMerchantCommissionAdapter creates a CommissionLookup that reads from cat_merchants.
func newMerchantCommissionAdapter() *merchantCommissionAdapter {
	return &merchantCommissionAdapter{
		repo: catalog_repo.NewMerchantRepo(),
	}
}

// GetCommissionBP returns the merchant's commission rate in basis points.
func (a *merchantCommissionAdapter) GetCommissionBP(ctx context.Context, merchantID id.ID) (int, error) {
	m, err := a.repo.GetByID(ctx, merchantID)
	if err != nil {
		return 0, fmt.Errorf("lookup merchant %s: %w", merchantID, err)
	}
	return m.CommissionRate, nil
}

// compile-time check
var _ crypto.CommissionLookup = (*merchantCommissionAdapter)(nil)
