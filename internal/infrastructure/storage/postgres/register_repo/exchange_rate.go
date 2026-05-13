package register_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/registers/exchange_rate"
	"metapus/internal/infrastructure/storage/postgres"
)

// ExchangeRateRepo implements exchange_rate.Repository.
type ExchangeRateRepo struct{}

// NewExchangeRateRepo creates a new exchange rate register repository.
func NewExchangeRateRepo() *ExchangeRateRepo {
	return &ExchangeRateRepo{}
}

// Upsert creates or updates a rate for (currency_id, date, rate_source_id).
func (r *ExchangeRateRepo) Upsert(ctx context.Context, rate *exchange_rate.ExchangeRate) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	const query = `
		INSERT INTO reg_exchange_rates (currency_id, date, rate, multiplier, rate_source_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now(), now())
		ON CONFLICT (currency_id, date, rate_source_id) DO UPDATE SET
			rate = EXCLUDED.rate,
			multiplier = EXCLUDED.multiplier,
			updated_at = now()
	`

	_, err := querier.Exec(ctx, query,
		rate.CurrencyID, rate.Date, rate.Rate, rate.Multiplier, rate.RateSourceID,
	)
	if err != nil {
		return fmt.Errorf("upsert exchange rate: %w", err)
	}

	return nil
}

// GetLatestRate returns the most recent rate for currency_id+rateSourceID where date <= asOf.
// Analogue of 1C "СрезПоследних".
func (r *ExchangeRateRepo) GetLatestRate(ctx context.Context, currencyID, rateSourceID id.ID, asOf time.Time) (*exchange_rate.ExchangeRate, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	const query = `
		SELECT currency_id, date, rate, multiplier, rate_source_id
		FROM reg_exchange_rates
		WHERE currency_id = $1 AND rate_source_id = $2 AND date <= $3
		ORDER BY date DESC
		LIMIT 1
	`

	var rate exchange_rate.ExchangeRate
	if err := pgxscan.Get(ctx, querier, &rate, query, currencyID, rateSourceID, asOf); err != nil {
		return nil, fmt.Errorf("get latest exchange rate: %w", err)
	}

	return &rate, nil
}

// GetRateOnDate returns the exact rate on a specific date.
func (r *ExchangeRateRepo) GetRateOnDate(ctx context.Context, currencyID, rateSourceID id.ID, date time.Time) (*exchange_rate.ExchangeRate, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	const query = `
		SELECT currency_id, date, rate, multiplier, rate_source_id
		FROM reg_exchange_rates
		WHERE currency_id = $1 AND rate_source_id = $2 AND date = $3
	`

	var rate exchange_rate.ExchangeRate
	if err := pgxscan.Get(ctx, querier, &rate, query, currencyID, rateSourceID, date); err != nil {
		return nil, fmt.Errorf("get exchange rate on date: %w", err)
	}

	return &rate, nil
}

// ListByCurrency returns all rates for a currency ordered by date DESC.
func (r *ExchangeRateRepo) ListByCurrency(ctx context.Context, currencyID, rateSourceID id.ID, limit int) ([]exchange_rate.ExchangeRate, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	const query = `
		SELECT currency_id, date, rate, multiplier, rate_source_id
		FROM reg_exchange_rates
		WHERE currency_id = $1 AND rate_source_id = $2
		ORDER BY date DESC
		LIMIT $3
	`

	var rates []exchange_rate.ExchangeRate
	if err := pgxscan.Select(ctx, querier, &rates, query, currencyID, rateSourceID, limit); err != nil {
		return nil, fmt.Errorf("list exchange rates: %w", err)
	}

	return rates, nil
}

// Compile-time interface check.
var _ exchange_rate.Repository = (*ExchangeRateRepo)(nil)
