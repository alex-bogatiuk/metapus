package documents

import (
	"context"
	"fmt"
	"sync"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/contract"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/domain/catalogs/organization"
)

// currencyTTL is the cache lifetime for contract/organization currency lookups.
// These rarely change, so a 5-minute cache is safe and eliminates N+1 in batch operations.
const currencyTTL = 5 * time.Minute

// cachedCurrencyID stores a resolved currency ID with an expiration time.
type cachedCurrencyID struct {
	currencyID id.ID
	hasValue   bool      // false when the source entity has no currency set
	expiresAt  time.Time
}

// CurrencyResolver provides logic to determine document currency.
// Resolution chain (1C-style): Document → Contract → Organization → System base currency.
//
// All intermediate lookups (contract currency, org currency) are cached in-memory
// with a short TTL to eliminate N+1 queries during batch document creation.
type CurrencyResolver struct {
	contracts  contract.Repository
	orgs       organization.Repository
	currencies currency.Repository

	// baseCurrency cache — avoids repeated DB lookups for the rarely-changing system currency.
	baseMu     sync.RWMutex
	baseID     id.ID
	baseCached bool

	// contractCurrencyCache caches Contract.CurrencyID by contract ID.
	// Key: id.ID, Value: *cachedCurrencyID
	contractCurrencyCache sync.Map

	// orgCurrencyCache caches Organization.BaseCurrencyID by org ID.
	// Key: id.ID, Value: *cachedCurrencyID
	orgCurrencyCache sync.Map
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
//  2. Currency from contract (cat_contracts.currency_id) — cached
//  3. Organization base currency (cat_organizations.base_currency_id) — cached
//  4. System base currency (cat_currencies where is_base = true) — cached
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

	// 2. Contract currency (with TTL cache)
	if contractID != nil && !id.IsNil(*contractID) {
		currID, found, err := r.resolveContractCurrency(ctx, *contractID)
		if err != nil {
			return id.Nil(), err
		}
		if found {
			return currID, nil
		}
	}

	// 3. Organization base currency (with TTL cache)
	if !id.IsNil(organizationID) {
		currID, found, err := r.resolveOrgCurrency(ctx, organizationID)
		if err != nil {
			return id.Nil(), err
		}
		if found {
			return currID, nil
		}
	}

	// 4. System base currency (cached, no TTL — only invalidated explicitly)
	return r.getBaseCurrency(ctx)
}

// resolveContractCurrency returns the contract's currency, using the TTL cache.
// Returns (currencyID, found, error). found=false means the contract has no currency set.
func (r *CurrencyResolver) resolveContractCurrency(ctx context.Context, contractID id.ID) (id.ID, bool, error) {
	// Fast path: check cache
	if cached, ok := r.contractCurrencyCache.Load(contractID); ok {
		entry := cached.(*cachedCurrencyID)
		if time.Now().Before(entry.expiresAt) {
			if entry.hasValue {
				return entry.currencyID, true, nil
			}
			return id.Nil(), false, nil
		}
		// Expired — fall through to DB
	}

	// Slow path: DB lookup
	c, err := r.contracts.GetByID(ctx, contractID)
	if err != nil {
		if apperror.IsNotFound(err) {
			// Cache the "not found" to avoid repeated lookups for invalid contract IDs
			r.contractCurrencyCache.Store(contractID, &cachedCurrencyID{
				hasValue:  false,
				expiresAt: time.Now().Add(currencyTTL),
			})
			return id.Nil(), false, nil
		}
		return id.Nil(), false, fmt.Errorf("resolve contract currency: %w", err)
	}

	if c != nil && c.CurrencyID != nil && !id.IsNil(*c.CurrencyID) {
		r.contractCurrencyCache.Store(contractID, &cachedCurrencyID{
			currencyID: *c.CurrencyID,
			hasValue:   true,
			expiresAt:  time.Now().Add(currencyTTL),
		})
		return *c.CurrencyID, true, nil
	}

	// Contract exists but has no currency — cache negative result
	r.contractCurrencyCache.Store(contractID, &cachedCurrencyID{
		hasValue:  false,
		expiresAt: time.Now().Add(currencyTTL),
	})
	return id.Nil(), false, nil
}

// resolveOrgCurrency returns the organization's base currency, using the TTL cache.
// Returns (currencyID, found, error). found=false means the org has no base currency.
func (r *CurrencyResolver) resolveOrgCurrency(ctx context.Context, orgID id.ID) (id.ID, bool, error) {
	// Fast path: check cache
	if cached, ok := r.orgCurrencyCache.Load(orgID); ok {
		entry := cached.(*cachedCurrencyID)
		if time.Now().Before(entry.expiresAt) {
			if entry.hasValue {
				return entry.currencyID, true, nil
			}
			return id.Nil(), false, nil
		}
	}

	// Slow path: DB lookup
	org, err := r.orgs.GetByID(ctx, orgID)
	if err != nil {
		if apperror.IsNotFound(err) {
			r.orgCurrencyCache.Store(orgID, &cachedCurrencyID{
				hasValue:  false,
				expiresAt: time.Now().Add(currencyTTL),
			})
			return id.Nil(), false, nil
		}
		return id.Nil(), false, fmt.Errorf("resolve organization currency: %w", err)
	}

	if org != nil && !id.IsNil(org.BaseCurrencyID) {
		r.orgCurrencyCache.Store(orgID, &cachedCurrencyID{
			currencyID: org.BaseCurrencyID,
			hasValue:   true,
			expiresAt:  time.Now().Add(currencyTTL),
		})
		return org.BaseCurrencyID, true, nil
	}

	r.orgCurrencyCache.Store(orgID, &cachedCurrencyID{
		hasValue:  false,
		expiresAt: time.Now().Add(currencyTTL),
	})
	return id.Nil(), false, nil
}

// getBaseCurrency returns the cached system base currency, fetching from DB on first call.
// Uses double-check locking pattern for thread safety with minimal contention.
func (r *CurrencyResolver) getBaseCurrency(ctx context.Context) (id.ID, error) {
	// Fast path: read lock
	r.baseMu.RLock()
	if r.baseCached {
		cached := r.baseID
		r.baseMu.RUnlock()
		return cached, nil
	}
	r.baseMu.RUnlock()

	// Slow path: write lock + double-check
	r.baseMu.Lock()
	defer r.baseMu.Unlock()

	if r.baseCached {
		return r.baseID, nil
	}

	base, err := r.currencies.GetBaseCurrency(ctx)
	if err != nil {
		return id.Nil(), fmt.Errorf("failed to determine currency: %w", err)
	}

	r.baseID = base.ID
	r.baseCached = true
	return base.ID, nil
}

// InvalidateBaseCurrency clears the cached base currency.
// Call this when the system base currency is changed (e.g., in currency catalog hooks).
func (r *CurrencyResolver) InvalidateBaseCurrency() {
	r.baseMu.Lock()
	r.baseCached = false
	r.baseMu.Unlock()
}

// InvalidateContractCurrency removes a specific contract from the currency cache.
// Call this when a contract's currency is updated.
func (r *CurrencyResolver) InvalidateContractCurrency(contractID id.ID) {
	r.contractCurrencyCache.Delete(contractID)
}

// InvalidateOrgCurrency removes a specific organization from the currency cache.
// Call this when an organization's base currency is updated.
func (r *CurrencyResolver) InvalidateOrgCurrency(orgID id.ID) {
	r.orgCurrencyCache.Delete(orgID)
}
