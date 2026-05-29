// Package crypto provides crypto-processing domain services.
// BalanceCalculator aggregates merchant crypto balances from the
// reg_crypto_merchant_balance register and converts them to a reporting currency
// using exchange rates from reg_exchange_rates.
package crypto

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/registers/exchange_rate"
)

// BalanceRow represents a single token balance from the accumulation register.
// Infrastructure layer populates this with joined token + currency data.
type BalanceRow struct {
	MerchantID    id.ID              `db:"merchant_id"`
	TokenID       id.ID              `db:"token_id"`
	TokenSymbol   string             `db:"symbol"`
	DecimalPlaces int                `db:"decimal_places"`
	CurrencyID    *id.ID             `db:"currency_id"` // nullable — token may not have currency linked
	CurrencyCode  string             `db:"iso_code"`    // "" if no currency
	RawAmount     types.CryptoAmount `db:"amount"`      // minor units from register
}

// BalanceQueryRepo provides read access to merchant balance data.
// Implemented by infrastructure layer (portal_repo).
type BalanceQueryRepo interface {
	// GetMerchantBalances returns token-level balances for the given merchants.
	// Joins reg_crypto_merchant_balance_balances with cat_tokens and cat_currencies.
	GetMerchantBalances(ctx context.Context, merchantIDs []id.ID) ([]BalanceRow, error)
}

// RateSourceResolver resolves a rate source code (e.g. "coingecko") to its UUID.
// Implemented by infrastructure layer to decouple domain from direct DB queries.
type RateSourceResolver interface {
	ResolveRateSourceID(ctx context.Context, code string) (id.ID, error)
}

// BalanceCalculator converts raw crypto balances to reporting currency values.
// Pure domain logic — no DB access (data injected via BalanceRow + ExchangeRate).
type BalanceCalculator struct {
	rateSvc *exchange_rate.Service
}

// NewBalanceCalculator creates a new balance calculator.
func NewBalanceCalculator(rateSvc *exchange_rate.Service) *BalanceCalculator {
	return &BalanceCalculator{rateSvc: rateSvc}
}

// MerchantBalance is the fully calculated balance for a merchant.
type MerchantBalance struct {
	TotalBase    decimal.Decimal // total in base currency
	BaseCurrency string          // e.g. "USD"
	RateSource   string          // e.g. "coingecko"
	ByToken      []TokenBalance  // breakdown per token
}

// TokenBalance represents a single token's balance with fiat valuation.
type TokenBalance struct {
	TokenID       id.ID
	TokenSymbol   string
	DecimalPlaces int             // token minor-unit scale, e.g. 6 for USDT-TRC20
	CurrencyCode  string          // from Token→Currency (e.g. "USDT")
	RawAmount     string          // minor units as string
	HumanAmount   decimal.Decimal // raw / 10^decimalPlaces
	Rate          decimal.Decimal // exchange rate to base currency
	Multiplier    int             // rate multiplier
	BaseAmount    decimal.Decimal // humanAmount * rate / multiplier
	HasRate       bool            // false if no exchange rate found
}

// Calculate converts raw balance rows to valued token balances.
// For each row: humanAmount = rawAmount.ToDecimal(decimals), baseAmount = humanAmount * rate / multiplier.
// Tokens without currency_id or without exchange rate are included with HasRate=false.
// rateSourceID is the primary source; fallbackRateSourceID is tried if primary fails.
func (c *BalanceCalculator) Calculate(ctx context.Context, rows []BalanceRow, rateSourceID id.ID, fallbackRateSourceID *id.ID) (*MerchantBalance, error) {
	result := &MerchantBalance{
		TotalBase: decimal.Zero,
		ByToken:   make([]TokenBalance, 0, len(rows)),
	}

	for _, row := range rows {
		tb := TokenBalance{
			TokenID:       row.TokenID,
			TokenSymbol:   row.TokenSymbol,
			DecimalPlaces: row.DecimalPlaces,
			CurrencyCode:  row.CurrencyCode,
			RawAmount:     row.RawAmount.String(),
		}

		// Convert minor units → human-readable amount via CryptoAmount.ToDecimal.
		// Example: 1_000_000 with decimal_places=6 → 1.000000
		tb.HumanAmount = row.RawAmount.ToDecimal(row.DecimalPlaces)

		// Look up exchange rate (only if currency is linked).
		if row.CurrencyID != nil && !id.IsNil(*row.CurrencyID) {
			rate, err := c.rateSvc.GetLatestRate(ctx, *row.CurrencyID, rateSourceID)
			if err != nil && fallbackRateSourceID != nil {
				// No rate found — try fallback source.
				rate, err = c.rateSvc.GetLatestRate(ctx, *row.CurrencyID, *fallbackRateSourceID)
			}
			if err == nil {
				tb.Rate = rate.Rate
				tb.Multiplier = rate.Multiplier
				tb.BaseAmount = rate.ToBaseAmount(tb.HumanAmount)
				tb.HasRate = true
				result.TotalBase = result.TotalBase.Add(tb.BaseAmount)
			}
		}

		result.ByToken = append(result.ByToken, tb)
	}

	return result, nil
}

// CalculateForMerchants is a convenience that loads balances + calculates in one call.
func (c *BalanceCalculator) CalculateForMerchants(
	ctx context.Context,
	repo BalanceQueryRepo,
	merchantIDs []id.ID,
	rateSourceID id.ID,
	fallbackRateSourceID *id.ID,
) (*MerchantBalance, error) {
	rows, err := repo.GetMerchantBalances(ctx, merchantIDs)
	if err != nil {
		return nil, fmt.Errorf("get merchant balances: %w", err)
	}

	return c.Calculate(ctx, rows, rateSourceID, fallbackRateSourceID)
}
