package documents

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/domain/catalogs/organization"
	"metapus/internal/domain/catalogs/warehouse"
)

// CurrencyResolver provides logic to determine document currency.
type CurrencyResolver struct {
	warehouses warehouse.Repository
	orgs       organization.Repository
	currencies currency.Repository
}

// NewCurrencyResolver creates a new CurrencyResolver.
func NewCurrencyResolver(
	warehouses warehouse.Repository,
	orgs organization.Repository,
	currencies currency.Repository,
) *CurrencyResolver {
	return &CurrencyResolver{
		warehouses: warehouses,
		orgs:       orgs,
		currencies: currencies,
	}
}

// ResolveForDocument determines the currency for a document based on explicit input,
// warehouse defaults, or organization defaults.
func (r *CurrencyResolver) ResolveForDocument(
	ctx context.Context,
	explicitCurrencyID id.ID,
	warehouseID id.ID,
	organizationID id.ID,
) (id.ID, error) {
	// 1. Explicit currency in document
	if !id.IsNil(explicitCurrencyID) {
		return explicitCurrencyID, nil
	}

	// 2. Warehouse default
	if !id.IsNil(warehouseID) {
		wh, err := r.warehouses.GetByID(ctx, warehouseID)
		if err == nil && wh != nil && wh.DefaultCurrencyID != nil {
			return *wh.DefaultCurrencyID, nil
		}
	}

	// 3. Organization base currency
	if !id.IsNil(organizationID) {
		org, err := r.orgs.GetByID(ctx, organizationID)
		if err == nil && org != nil && !id.IsNil(org.BaseCurrencyID) {
			return org.BaseCurrencyID, nil
		}
	}

	// 4. System base currency
	base, err := r.currencies.GetBaseCurrency(ctx)
	if err != nil {
		return id.Nil(), fmt.Errorf("failed to determine currency: %w", err)
	}

	return base.ID, nil
}
