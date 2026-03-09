// Package vat_rate provides the VATRate catalog (Справочник "Ставки НДС").
// VATRates represent tax rates for goods and services.
package vat_rate

import (
	"context"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
)

// VATRate represents a VAT (НДС) rate entry.
type VATRate struct {
	entity.Catalog

	// Rate is the VAT percentage (e.g., 20, 10, 0)
	Rate decimal.Decimal `db:"rate" json:"rate" meta:"label:Ставка"`

	// IsTaxExempt indicates operations exempt from VAT (без НДС)
	IsTaxExempt bool `db:"is_tax_exempt" json:"isTaxExempt" meta:"label:Без НДС"`

	// Description is an optional note
	Description *string `db:"description" json:"description,omitempty" meta:"label:Описание"`
}

// NewVATRate creates a new VATRate with required fields.
func NewVATRate(code, name string, rate decimal.Decimal) *VATRate {
	return &VATRate{
		Catalog: entity.NewCatalog(code, name),
		Rate:    rate,
	}
}

// Validate implements entity.Validatable interface.
func (v *VATRate) Validate(ctx context.Context) error {
	// Base catalog validation
	if err := v.Catalog.Validate(ctx); err != nil {
		return err
	}

	// Rate must be non-negative
	if v.Rate.IsNegative() {
		return apperror.NewValidation("VAT rate must be non-negative").
			WithDetail("field", "rate")
	}

	// If tax exempt, rate should be zero
	if v.IsTaxExempt && !v.Rate.IsZero() {
		return apperror.NewValidation("tax exempt rate must be zero").
			WithDetail("field", "rate")
	}

	return nil
}

// RatePercent returns the rate as an integer percent (e.g., 20 for 20%).
func (v *VATRate) RatePercent() int {
	return int(v.Rate.IntPart())
}
