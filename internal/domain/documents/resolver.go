package documents

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/contract"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/domain/catalogs/organization"
)

// CurrencyResolver provides logic to determine document currency.
// Resolution chain (1C-style): Document → Contract → Organization → System base currency.
type CurrencyResolver struct {
	contracts  contract.Repository
	orgs       organization.Repository
	currencies currency.Repository
}

// NewCurrencyResolver creates a new CurrencyResolver.
func NewCurrencyResolver(
	contracts contract.Repository,
	orgs organization.Repository,
	currencies currency.Repository,
) *CurrencyResolver {
	return &CurrencyResolver{
		contracts:  contracts,
		orgs:       orgs,
		currencies: currencies,
	}
}

// ResolveForDocument determines the currency for a document based on explicit input,
// contract currency, or organization defaults.
// Resolution chain (1C-style):
//  1. Explicit currency in document
//  2. Currency from contract (cat_contracts.currency_id)
//  3. Organization base currency (cat_organizations.base_currency_id)
//  4. System base currency (cat_currencies where is_base = true)
func (r *CurrencyResolver) ResolveForDocument(
	ctx context.Context,
	explicitCurrencyID id.ID,
	contractID *id.ID,
	organizationID id.ID,
) (id.ID, error) {
	// 1. Explicit currency in document
	if !id.IsNil(explicitCurrencyID) {
		return explicitCurrencyID, nil
	}

	// 2. Contract currency
	if contractID != nil && !id.IsNil(*contractID) {
		c, err := r.contracts.GetByID(ctx, *contractID)
		if err != nil {
			if !apperror.IsNotFound(err) {
				return id.Nil(), fmt.Errorf("resolve contract currency: %w", err)
			}
		} else if c != nil && c.CurrencyID != nil && !id.IsNil(*c.CurrencyID) {
			return *c.CurrencyID, nil
		}
	}

	// 3. Organization base currency
	if !id.IsNil(organizationID) {
		org, err := r.orgs.GetByID(ctx, organizationID)
		if err != nil {
			if !apperror.IsNotFound(err) {
				return id.Nil(), fmt.Errorf("resolve organization currency: %w", err)
			}
		} else if org != nil && !id.IsNil(org.BaseCurrencyID) {
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
