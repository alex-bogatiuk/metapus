package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/patrickmn/go-cache"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// CurrencyMetadataResolverImpl implements domain.CurrencyMetadataResolver.
// It caches currency info in memory to avoid redundant DB queries during outbox event building.
type CurrencyMetadataResolverImpl struct {
	pool  *pgxpool.Pool
	cache *cache.Cache
}

// NewCurrencyMetadataResolver creates a new currency metadata resolver with a 1-hour cache.
func NewCurrencyMetadataResolver(pool *pgxpool.Pool) *CurrencyMetadataResolverImpl {
	// Currencies rarely change their decimal places, so a long TTL is safe.
	return &CurrencyMetadataResolverImpl{
		pool:  pool,
		cache: cache.New(1*time.Hour, 2*time.Hour),
	}
}

// ResolveCurrency gets currency metadata by ID.
func (r *CurrencyMetadataResolverImpl) ResolveCurrency(ctx context.Context, currencyID id.ID) (*domain.CurrencyInfo, error) {
	if id.IsNil(currencyID) {
		return nil, fmt.Errorf("currencyID is nil")
	}

	key := currencyID.String()

	// 1. Try memory cache first
	if val, found := r.cache.Get(key); found {
		if info, ok := val.(*domain.CurrencyInfo); ok {
			return info, nil
		}
	}

	// 2. Fetch from DB
	// We use the tenant pool. Even though this runs inside an outbox tx,
	// the outbox payload building runs BEFORE the tx is committed, or in its own tx.
	// Since currency metadata is tenant-global and immutable across txs, we can use the pool directly
	// or the active tx if necessary. To be safe with any context, we use the active tx if present.
	
	querier := Querier(r.pool)
	txm := MustGetTxManager(ctx)
	if tx := txm.GetTx(ctx); tx != nil {
		querier = tx
	}

	var decimalPlaces int
	var symbol string
	var name string

	query := `SELECT decimal_places, COALESCE(symbol, ''), COALESCE(name, code, '') 
	          FROM cat_currencies WHERE id = $1`
	
	err := querier.QueryRow(ctx, query, currencyID).Scan(&decimalPlaces, &symbol, &name)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch currency metadata for %s: %w", currencyID, err)
	}

	info := &domain.CurrencyInfo{
		DecimalPlaces: decimalPlaces,
		Symbol:        symbol,
		Name:          name,
	}

	// 3. Update cache
	r.cache.Set(key, info, cache.DefaultExpiration)

	return info, nil
}
