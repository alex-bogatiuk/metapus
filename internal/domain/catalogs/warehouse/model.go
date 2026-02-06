// Package warehouse provides the Warehouse catalog (Справочник "Склады").
// Warehouses represent physical locations for storing goods and inventory.
package warehouse

import (
	"context"
	"regexp"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// WarehouseType defines the type of warehouse.
type WarehouseType string

const (
	TypeMain         WarehouseType = "main"         // Основной склад
	TypeDistribution WarehouseType = "distribution" // Распределительный центр
	TypeRetail       WarehouseType = "retail"       // Розничный склад/магазин
	TypeProduction   WarehouseType = "production"   // Производственный склад
	TypeTransit      WarehouseType = "transit"      // Транзитный склад
)

// Warehouse represents a storage location for goods.
type Warehouse struct {
	entity.Catalog

	// Type defines the warehouse category
	Type WarehouseType `db:"type" json:"type"`

	// Address is the physical address
	Address *string `db:"address" json:"address,omitempty"`

	// IsActive indicates if warehouse is operational
	IsActive bool `db:"is_active" json:"isActive"`

	// AllowNegativeStock indicates if negative stock is allowed
	AllowNegativeStock bool `db:"allow_negative_stock" json:"allowNegativeStock"`

	// IsDefault indicates if this is the default warehouse
	IsDefault bool `db:"is_default" json:"isDefault"`

	// OrganizationID is reference to owning organization
	OrganizationID string `db:"organization_id" json:"organizationId,omitempty"`

	// Description
	Description *string `db:"description" json:"description,omitempty"`

	// DefaultCurrencyID is the default currency for documents on this warehouse
	DefaultCurrencyID *id.ID `db:"default_currency_id" json:"defaultCurrencyId,omitempty"`
}

// NewWarehouse creates a new Warehouse with required fields.
func NewWarehouse(code, name string, whType WarehouseType) *Warehouse {
	return &Warehouse{
		Catalog:  entity.NewCatalog(code, name),
		Type:     whType,
		IsActive: true,
	}
}

// Validate implements entity.Validatable interface.
func (w *Warehouse) Validate(ctx context.Context) error {
	// Base catalog validation
	if err := w.Catalog.Validate(ctx); err != nil {
		return err
	}

	// Type validation
	if !isValidWarehouseType(w.Type) {
		return apperror.NewValidation("invalid warehouse type").
			WithDetail("field", "type").
			WithDetail("value", string(w.Type))
	}

	return nil
}

// CanAcceptStock returns true if warehouse can accept stock.
func (w *Warehouse) CanAcceptStock() bool {
	return w.IsActive && !w.IsFolder
}

// CanIssueStock returns true if warehouse can issue stock.
func (w *Warehouse) CanIssueStock(negativeAllowed bool) bool {
	return w.IsActive && !w.IsFolder && (negativeAllowed || w.AllowNegativeStock)
}

// --- Validation Helpers ---

func isValidWarehouseType(t WarehouseType) bool {
	switch t {
	case TypeMain, TypeDistribution, TypeRetail, TypeProduction, TypeTransit:
		return true
	}
	return false
}

func isValidEmail(email string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`).MatchString(email)
}
