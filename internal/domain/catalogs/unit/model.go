// Package unit provides the Unit catalog (Справочник "Единицы измерения").
// Units represent measurement units for products and goods.
package unit

import (
	"context"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
)

// UnitType defines the type of measurement unit.
type UnitType string

const (
	TypePiece  UnitType = "piece"  // Штуки
	TypeWeight UnitType = "weight" // Вес (кг, г, т)
	TypeLength UnitType = "length" // Длина (м, см, мм)
	TypeArea   UnitType = "area"   // Площадь (м², см²)
	TypeVolume UnitType = "volume" // Объём (л, мл, м³)
	TypeTime   UnitType = "time"   // Время (ч, мин, сек)
	TypePack   UnitType = "pack"   // Упаковки
)

// Unit represents a measurement unit.
type Unit struct {
	entity.Catalog

	// Type defines the unit category
	Type UnitType `db:"type" json:"type"`

	// Symbol is the short symbol (e.g., "kg", "m", "pcs")
	Symbol string `db:"symbol" json:"symbol"`

	// InternationalCode is the OKEI code (Russian classifier)
	InternationalCode *string `db:"international_code" json:"internationalCode,omitempty"`

	// BaseUnitID is reference to base unit for conversions
	BaseUnitID *string `db:"base_unit_id" json:"baseUnitId,omitempty"`

	// ConversionFactor is the multiplier to convert to base unit
	// e.g., for "gram" with base "kilogram": factor = 0.001
	ConversionFactor decimal.Decimal `db:"conversion_factor" json:"conversionFactor"`

	// IsBase indicates if this is a base unit (not derived)
	IsBase bool `db:"is_base" json:"isBase"`

	// Description is a free-form note
	Description *string `db:"description" json:"description,omitempty"`
}

// NewUnit creates a new Unit with required fields.
func NewUnit(code, name, symbol string, unitType UnitType) *Unit {
	return &Unit{
		Catalog:          entity.NewCatalog(code, name),
		Type:             unitType,
		Symbol:           symbol,
		ConversionFactor: decimal.NewFromInt(1),
		IsBase:           true,
	}
}

// Validate implements entity.Validatable interface.
func (u *Unit) Validate(ctx context.Context) error {
	// Base catalog validation
	if err := u.Catalog.Validate(ctx); err != nil {
		return err
	}

	// Symbol is required
	if u.Symbol == "" {
		return apperror.NewValidation("symbol is required").
			WithDetail("field", "symbol")
	}

	// Type validation
	if !isValidUnitType(u.Type) {
		return apperror.NewValidation("invalid unit type").
			WithDetail("field", "type").
			WithDetail("value", string(u.Type))
	}

	// Conversion factor must be positive
	if !u.ConversionFactor.IsPositive() {
		return apperror.NewValidation("conversion factor must be positive").
			WithDetail("field", "conversionFactor")
	}

	// If base unit is set, this is not a base unit
	if u.BaseUnitID != nil && *u.BaseUnitID != "" && u.IsBase {
		return apperror.NewValidation("unit with base unit reference cannot be marked as base").
			WithDetail("field", "isBase")
	}

	return nil
}

// ConvertTo converts a quantity from this unit to target unit.
// Returns the converted quantity and error if conversion not possible.
func (u *Unit) ConvertTo(qty decimal.Decimal, target *Unit) (decimal.Decimal, error) {
	// Both must be same type for conversion
	if u.Type != target.Type {
		return decimal.Zero, apperror.NewValidation("cannot convert between different unit types").
			WithDetail("source", string(u.Type)).
			WithDetail("target", string(target.Type))
	}

	// Convert to base unit first, then to target
	// qty * source.factor / target.factor
	result := qty.Mul(u.ConversionFactor).Div(target.ConversionFactor)
	return result.Round(int32(3)), nil
}

// --- Validation Helpers ---

func isValidUnitType(t UnitType) bool {
	switch t {
	case TypePiece, TypeWeight, TypeLength, TypeArea, TypeVolume, TypeTime, TypePack:
		return true
	}
	return false
}
