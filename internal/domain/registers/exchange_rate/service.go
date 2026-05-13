// Package exchange_rate provides the exchange rates information register.
// Periodic register (analogue of 1C "РегистрСведений.КурсыВалют"):
// stores currency rates to base currency by date and source.
package exchange_rate

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// ExchangeRate represents a single rate record in the information register.
type ExchangeRate struct {
	CurrencyID   id.ID           `db:"currency_id" json:"currencyId"`
	Date         time.Time       `db:"date" json:"date"`
	Rate         decimal.Decimal `db:"rate" json:"rate"`
	Multiplier   int             `db:"multiplier" json:"multiplier"`
	RateSourceID id.ID           `db:"rate_source_id" json:"rateSourceId"`
}

// ToBaseAmount converts an amount in this currency to the base currency.
// Formula: amount * rate / multiplier.
// Example: 100 JPY, rate=0.67, multiplier=100 → 100 * 0.67 / 100 = 0.67 USD.
func (r *ExchangeRate) ToBaseAmount(amount decimal.Decimal) decimal.Decimal {
	return amount.Mul(r.Rate).Div(decimal.NewFromInt(int64(r.Multiplier)))
}

// Repository defines storage operations for exchange rates.
type Repository interface {
	// Upsert creates or updates a rate for (currency_id, date, rate_source_id).
	Upsert(ctx context.Context, rate *ExchangeRate) error

	// GetLatestRate returns the most recent rate for currency_id+rateSourceID where date <= asOf.
	// Analogue of 1C "СрезПоследних".
	GetLatestRate(ctx context.Context, currencyID, rateSourceID id.ID, asOf time.Time) (*ExchangeRate, error)

	// GetRateOnDate returns the exact rate on a specific date.
	GetRateOnDate(ctx context.Context, currencyID, rateSourceID id.ID, date time.Time) (*ExchangeRate, error)

	// ListByCurrency returns all rates for a currency ordered by date DESC.
	ListByCurrency(ctx context.Context, currencyID, rateSourceID id.ID, limit int) ([]ExchangeRate, error)
}

// Service provides business operations for the exchange rates register.
type Service struct {
	repo Repository
}

// NewService creates a new exchange rate register service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// UpsertRate creates or updates a rate record.
func (s *Service) UpsertRate(ctx context.Context, rate *ExchangeRate) error {
	if rate.Rate.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("exchange rate must be positive: %s", rate.Rate)
	}
	if rate.Multiplier < 1 {
		return fmt.Errorf("multiplier must be >= 1: %d", rate.Multiplier)
	}
	if id.IsNil(rate.RateSourceID) {
		return fmt.Errorf("rate source ID is required")
	}

	if err := s.repo.Upsert(ctx, rate); err != nil {
		return fmt.Errorf("upsert exchange rate: %w", err)
	}

	logger.Info(ctx, "exchange rate updated",
		"currency_id", rate.CurrencyID,
		"date", rate.Date.Format("2006-01-02"),
		"rate", rate.Rate.String(),
		"rate_source_id", rate.RateSourceID,
	)
	return nil
}

// GetLatestRate returns the most recent rate for a currency from a specific source.
// Analogue of 1C "СрезПоследних(Дата)".
func (s *Service) GetLatestRate(ctx context.Context, currencyID, rateSourceID id.ID) (*ExchangeRate, error) {
	return s.repo.GetLatestRate(ctx, currencyID, rateSourceID, time.Now().UTC())
}

// GetRateOnDate returns the rate on a specific date (for retrospective recalculation).
func (s *Service) GetRateOnDate(ctx context.Context, currencyID id.ID, date time.Time, rateSourceID id.ID) (*ExchangeRate, error) {
	return s.repo.GetLatestRate(ctx, currencyID, rateSourceID, date)
}
