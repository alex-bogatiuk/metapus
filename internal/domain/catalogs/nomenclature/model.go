// Package nomenclature provides the Nomenclature catalog (Справочник "Номенклатура").
// Nomenclature represents products, goods, services, and other items.
package nomenclature

import (
	"context"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
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

// Nomenclature represents a product, good, service, or other item.
type Nomenclature struct {
	entity.Catalog

	// Type defines item category
	Type NomenclatureType `db:"type" json:"type" meta:"label:Тип"`

	// Article is the item article/SKU
	Article *string `db:"article" json:"article,omitempty" meta:"label:Артикул"`

	// Barcode is the item barcode (EAN-13, etc.)
	Barcode *string `db:"barcode" json:"barcode,omitempty" meta:"label:Штрихкод"`

	// BaseUnitID is the reference to base unit of measure
	BaseUnitID *string `db:"base_unit_id" json:"baseUnitId,omitempty" meta:"label:Базовая единица"`

	// DefaultVatRateID is the reference to default VAT rate from cat_vat_rates
	DefaultVatRateID *id.ID `db:"default_vat_rate_id" json:"defaultVatRateId,omitempty" meta:"label:Ставка НДС"`

	// Weight in kg (for logistics)
	Weight decimal.Decimal `db:"weight" json:"weight" meta:"label:Вес"`

	// Volume in cubic meters (for logistics)
	Volume decimal.Decimal `db:"volume" json:"volume" meta:"label:Объём"`

	// Description is a detailed description
	Description *string `db:"description" json:"description,omitempty" meta:"label:Описание"`

	// ManufacturerID is reference to manufacturer (counterparty)
	ManufacturerID *string `db:"manufacturer_id" json:"manufacturerId,omitempty" meta:"label:Производитель"`

	// CountryOfOrigin is the country code (ISO 3166-1 alpha-2)
	CountryOfOrigin *string `db:"country_of_origin" json:"countryOfOrigin,omitempty" meta:"label:Страна происхождения"`

	// IsWeighed indicates if item is sold by weight
	IsWeighed bool `db:"is_weighed" json:"isWeighed" meta:"label:Весовой"`

	// TrackSerial indicates if item is tracked by serial numbers
	TrackSerial bool `db:"track_serial" json:"trackSerial" meta:"label:Серийный учёт"`

	// TrackBatch indicates if item is tracked by batch/lot numbers
	TrackBatch bool `db:"track_batch" json:"trackBatch" meta:"label:Партионный учёт"`

	// ImageURL is the item image URL
	ImageURL *string `db:"image_url" json:"imageUrl,omitempty" meta:"label:Изображение"`
}

// NewNomenclature creates a new Nomenclature with required fields.
func NewNomenclature(code, name string, itemType NomenclatureType) *Nomenclature {
	return &Nomenclature{
		Catalog: entity.NewCatalog(code, name),
		Type:    itemType,
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

// --- Validation Helpers ---

func isValidNomenclatureType(t NomenclatureType) bool {
	switch t {
	case TypeGoods, TypeService, TypeWork, TypeMaterial, TypeSemiProduct, TypeProduct:
		return true
	}
	return false
}

