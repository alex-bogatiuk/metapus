// Package currency provides the Currency catalog.
// Currencies represent monetary units with exchange rates.
package currency

import (
	"context"
	"math"
	"regexp"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/types"
)

// Currency represents a monetary unit.
type Currency struct {
	entity.Catalog

	// ISOCode is the ISO 4217 alphabetic code or crypto ticker (e.g., "USD", "PYUSD").
	ISOCode *string `db:"iso_code" json:"isoCode" meta:"label:ISO-код / тикер"`

	// ISONumericCode is an optional three-digit ISO 4217 code (e.g., "840", "008").
	ISONumericCode *string `db:"iso_numeric_code" json:"isoNumericCode,omitempty" meta:"label:Цифровой код ISO (3 цифры)"`

	// Symbol is the currency symbol (e.g., "$", "€", "₽")
	Symbol *string `db:"symbol" json:"symbol" meta:"label:Символ"`

	// DecimalPlaces is the number of decimal places
	DecimalPlaces int `db:"decimal_places" json:"decimalPlaces" meta:"label:Знаков после запятой"`

	// MinorMultiplier is calculated as 10^DecimalPlaces, never entered in a form.
	MinorMultiplier int64 `db:"minor_multiplier" json:"-"`

	// IsBase indicates if this is the base (accounting) currency
	IsBase bool `db:"is_base" json:"isBase" meta:"label:Базовая"`

	// Country is the primary country for this currency
	Country *string `db:"country" json:"country,omitempty" meta:"label:Страна"`
}

// NewCurrency creates a new Currency with required fields.
// In Database-per-Tenant architecture, tenantID is not required.
func NewCurrency(code, name string, isoCode, symbol *string) *Currency {
	return &Currency{
		Catalog:       entity.NewCatalog(code, name),
		ISOCode:       isoCode,
		Symbol:        symbol,
		DecimalPlaces: 2,
	}
}

// Validate implements entity.Validatable interface.
func (c *Currency) Validate(ctx context.Context) error {
	// Base catalog validation
	if err := c.Catalog.Validate(ctx); err != nil {
		return err
	}

	// ISO code or crypto ticker is required and must be 2–5 uppercase letters.
	if !isValidISOCode(c.ISOCode) {
		return apperror.NewValidation("ISO-код или тикер должен содержать от 2 до 5 заглавных латинских букв").
			WithDetail("field", "isoCode").
			WithDetail("value", c.ISOCode)
	}

	// Crypto tickers may omit the numeric ISO code. Validate before persistence:
	// cat_currencies.iso_numeric_code is VARCHAR(3).
	if c.ISONumericCode != nil && *c.ISONumericCode != "" && !_isoNumericCodeRegex.MatchString(*c.ISONumericCode) {
		return apperror.NewValidation("Цифровой код ISO должен содержать ровно 3 цифры. Если кода нет, оставьте поле пустым").
			WithDetail("field", "isoNumericCode")
	}

	// Symbol is required
	if c.Symbol == nil || *c.Symbol == "" {
		return apperror.NewValidation("symbol is required").
			WithDetail("field", "symbol")
	}

	// Decimal places must be non-negative.
	// Max 9: supports Gwei precision (10^9 fits int32/int4).
	// For higher precision crypto (18 decimals), use cat_tokens.decimal_places instead.
	if c.DecimalPlaces < 0 || c.DecimalPlaces > 9 {
		return apperror.NewValidation("decimal places must be between 0 and 9").
			WithDetail("field", "decimalPlaces")
	}

	return nil
}

// Format formats an amount according to currency settings.
func (c *Currency) Format(amount decimal.Decimal) string {
	// Round to decimal places
	rounded := amount.Round(int32(c.DecimalPlaces))

	// Format with separators (simplified)
	formatted := rounded.StringFixed(int32(c.DecimalPlaces))

	return formatted + *c.Symbol
}

// ToMinorUnits converts major units (decimal) to minor units using currency's decimal places.
func (c *Currency) ToMinorUnits(major decimal.Decimal) (types.MinorUnits, error) {
	return types.CheckedMinorUnitsFromDecimal(major, c.DecimalPlaces)
}

// FromMinorUnits converts minor units to major units as decimal.Decimal.
func (c *Currency) FromMinorUnits(minor types.MinorUnits) decimal.Decimal {
	return minor.ToDecimal(c.DecimalPlaces)
}

// --- Validation Helpers ---

// _isoCodeRegex validates ISO code: 2–5 uppercase Latin letters.
// Covers ISO 4217 (USD, EUR) and crypto tickers (USDT, MATIC).
var _isoCodeRegex = regexp.MustCompile(`^[A-Z]{2,5}$`)

var _isoNumericCodeRegex = regexp.MustCompile(`^[0-9]{3}$`)

func isValidISOCode(code *string) bool {
	if code == nil {
		return false
	}
	return _isoCodeRegex.MatchString(*code)
}

func (c *Currency) Normalize() {
	if c.DecimalPlaces >= 0 && c.DecimalPlaces <= 9 {
		c.MinorMultiplier = int64(math.Pow10(c.DecimalPlaces))
	}
}
