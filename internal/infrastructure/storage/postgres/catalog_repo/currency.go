package catalog_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/infrastructure/storage/postgres"
)

const currencyTable = "cat_currencies"

// CurrencyRepo implements currency.Repository.
type CurrencyRepo struct {
	*BaseCatalogRepo[*currency.Currency]
}

// NewCurrencyRepo creates a new currency repository.
// In Database-per-Tenant architecture, TxManager is obtained from context per-request.
func NewCurrencyRepo() *CurrencyRepo {
	return &CurrencyRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*currency.Currency](
			currencyTable,
			postgres.ExtractDBColumns[currency.Currency](),
		),
	}
}

// FindByISOCode retrieves currency by ISO code.
func (r *CurrencyRepo) FindByISOCode(ctx context.Context, isoCode string) (*currency.Currency, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"iso_code": isoCode}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var c currency.Currency
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &c, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("currency", isoCode)
		}
		return nil, fmt.Errorf("find by iso code: %w", err)
	}

	return &c, nil
}

// UpdateExchangeRate updates the exchange rate for a currency.
func (r *CurrencyRepo) UpdateExchangeRate(ctx context.Context, currencyID id.ID, rate decimal.Decimal, date time.Time) error {
	q := r.Builder().
		Update(currencyTable).
		Set("exchange_rate", rate).
		Set("exchange_rate_date", date).
		Set("updated_at", time.Now()).
		Where(squirrel.Eq{"id": currencyID})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	_, err = querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update exchange rate: %w", err)
	}

	return nil
}

// ClearBase clears the base flag on all currencies.
func (r *CurrencyRepo) ClearBase(ctx context.Context) error {
	q := r.Builder().
		Update(currencyTable).
		Set("is_base", false).
		Where(squirrel.Eq{"is_base": true})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	_, err = querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("clear base: %w", err)
	}

	return nil
}
