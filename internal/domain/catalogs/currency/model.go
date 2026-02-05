// Package currency provides the Currency catalog (Справочник "Валюты").
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

	// ISOCode is the ISO 4217 alphabetic code (e.g., "USD", "EUR", "RUB")
	ISOCode *string `db:"iso_code" json:"isoCode"`

	// ISONumericCode is the ISO 4217 numeric code (e.g., 840, 978, 643)
	ISONumericCode *string `db:"iso_numeric_code" json:"isoNumericCode,omitempty"`

	// Symbol is the currency symbol (e.g., "$", "€", "₽")
	Symbol *string `db:"symbol" json:"symbol"`

	// DecimalPlaces is the number of decimal places
	DecimalPlaces int `db:"decimal_places" json:"decimalPlaces"`

	// MinorMultiplier is 10^DecimalPlaces (e.g., 100 for 2 decimal places)
	MinorMultiplier int64 `db:"minor_multiplier" json:"minorMultiplier"`

	// IsBase indicates if this is the base (accounting) currency
	IsBase bool `db:"is_base" json:"isBase"`

	// Country is the primary country for this currency
	Country *string `db:"country" json:"country,omitempty"`
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

	// ISO code is required and must be 3 uppercase letters
	if !isValidISOCode(c.ISOCode) {
		return apperror.NewValidation("ISO code must be 3 uppercase letters").
			WithDetail("field", "isoCode").
			WithDetail("value", c.ISOCode)
	}

	// Symbol is required
	if c.Symbol == nil || *c.Symbol == "" {
		return apperror.NewValidation("symbol is required").
			WithDetail("field", "symbol")
	}

	// Decimal places must be non-negative
	if c.DecimalPlaces < 0 || c.DecimalPlaces > 8 {
		return apperror.NewValidation("decimal places must be between 0 and 8").
			WithDetail("field", "decimalPlaces")
	}

	// Auto-calculate MinorMultiplier
	c.MinorMultiplier = int64(math.Pow10(c.DecimalPlaces))

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

// ToMinorUnits converts major units to minor units using currency's decimal places.
func (c *Currency) ToMinorUnits(major float64) types.MinorUnits {
	return types.NewMinorUnitsFromMajor(major, c.DecimalPlaces)
}

// FromMinorUnits converts minor units to major units for display.
func (c *Currency) FromMinorUnits(minor types.MinorUnits) float64 {
	return minor.ToMajor(c.DecimalPlaces)
}

// --- Validation Helpers ---

func isValidISOCode(code *string) bool {
	if code == nil {
		return false
	}
	return regexp.MustCompile(`^[A-Z]{3}$`).MatchString(*code)
}
