// Package nomenclature provides the Nomenclature catalog (Справочник "Номенклатура").
// Nomenclature represents products, goods, services, and other items.
package nomenclature

import (
	"context"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
)

// NomenclatureType defines the type of item.
type NomenclatureType string

const (
	TypeGoods       NomenclatureType = "goods"    // Товар
	TypeService     NomenclatureType = "service"  // Услуга
	TypeWork        NomenclatureType = "work"     // Работа
	TypeMaterial    NomenclatureType = "material" // Материал
	TypeSemiProduct NomenclatureType = "semi"     // Полуфабрикат
	TypeProduct     NomenclatureType = "product"  // Продукция
)

// VATRate defines VAT (НДС) rate.
type VATRate string

const (
	VAT0  VATRate = "0"  // Без НДС
	VAT10 VATRate = "10" // 10%
	VAT20 VATRate = "20" // 20%
)

// Nomenclature represents a product, good, service, or other item.
type Nomenclature struct {
	entity.Catalog

	// Type defines item category
	Type NomenclatureType `db:"type" json:"type"`

	// Article is the item article/SKU
	Article *string `db:"article" json:"article,omitempty"`

	// Barcode is the item barcode (EAN-13, etc.)
	Barcode *string `db:"barcode" json:"barcode,omitempty"`

	// BaseUnitID is the reference to base unit of measure
	BaseUnitID *string `db:"base_unit_id" json:"baseUnitId,omitempty"`

	// VATRate is the default VAT rate
	VATRate VATRate `db:"vat_rate" json:"vatRate"`

	// Weight in kg (for logistics)
	Weight decimal.Decimal `db:"weight" json:"weight"`

	// Volume in cubic meters (for logistics)
	Volume decimal.Decimal `db:"volume" json:"volume"`

	// Description is a detailed description
	Description *string `db:"description" json:"description,omitempty"`

	// ManufacturerID is reference to manufacturer (counterparty)
	ManufacturerID *string `db:"manufacturer_id" json:"manufacturerId,omitempty"`

	// CountryOfOrigin is the country code (ISO 3166-1 alpha-2)
	CountryOfOrigin *string `db:"country_of_origin" json:"countryOfOrigin,omitempty"`

	// IsWeighed indicates if item is sold by weight
	IsWeighed bool `db:"is_weighed" json:"isWeighed"`

	// TrackSerial indicates if item is tracked by serial numbers
	TrackSerial bool `db:"track_serial" json:"trackSerial"`

	// TrackBatch indicates if item is tracked by batch/lot numbers
	TrackBatch bool `db:"track_batch" json:"trackBatch"`

	// ImageURL is the item image URL
	ImageURL *string `db:"image_url" json:"imageUrl,omitempty"`
}

// NewNomenclature creates a new Nomenclature with required fields.
func NewNomenclature(code, name string, itemType NomenclatureType) *Nomenclature {
	return &Nomenclature{
		Catalog: entity.NewCatalog(code, name),
		Type:    itemType,
		VATRate: VAT20, // Default VAT rate
		Weight:  decimal.Zero,
		Volume:  decimal.Zero,
	}
}

// Validate implements entity.Validatable interface.
func (n *Nomenclature) Validate(ctx context.Context) error {
	// Base catalog validation
	if err := n.Catalog.Validate(ctx); err != nil {
		return err
	}

	// Type validation
	if !isValidNomenclatureType(n.Type) {
		return apperror.NewValidation("invalid nomenclature type").
			WithDetail("field", "type").
			WithDetail("value", string(n.Type))
	}

	// VAT rate validation
	if !isValidVATRate(n.VATRate) {
		return apperror.NewValidation("invalid VAT rate").
			WithDetail("field", "vatRate").
			WithDetail("value", string(n.VATRate))
	}

	// Weight must be non-negative
	if n.Weight.IsNegative() {
		return apperror.NewValidation("weight cannot be negative").
			WithDetail("field", "weight")
	}

	// Volume must be non-negative
	if n.Volume.IsNegative() {
		return apperror.NewValidation("volume cannot be negative").
			WithDetail("field", "volume")
	}

	// Services and works cannot be tracked by serial/batch
	if n.Type == TypeService || n.Type == TypeWork {
		if n.TrackSerial || n.TrackBatch {
			return apperror.NewValidation("services and works cannot be tracked by serial or batch").
				WithDetail("field", "type")
		}
	}

	return nil
}

// IsPhysical returns true if item has physical presence (not a service).
func (n *Nomenclature) IsPhysical() bool {
	return n.Type != TypeService && n.Type != TypeWork
}

// IsTracked returns true if item requires tracking.
func (n *Nomenclature) IsTracked() bool {
	return n.TrackSerial || n.TrackBatch
}

// GetVATMultiplier returns VAT multiplier for calculations.
func (n *Nomenclature) GetVATMultiplier() decimal.Decimal {
	switch n.VATRate {
	case VAT10:
		return decimal.NewFromFloat(0.10)
	case VAT20:
		return decimal.NewFromFloat(0.20)
	default:
		return decimal.Zero
	}
}

// --- Validation Helpers ---

func isValidNomenclatureType(t NomenclatureType) bool {
	switch t {
	case TypeGoods, TypeService, TypeWork, TypeMaterial, TypeSemiProduct, TypeProduct:
		return true
	}
	return false
}

func isValidVATRate(r VATRate) bool {
	switch r {
	case VAT0, VAT10, VAT20:
		return true
	}
	return false
}
