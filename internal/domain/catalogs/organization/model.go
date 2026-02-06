// Package organization provides the Organization catalog (Справочник "Организации").
package organization

import (
	"context"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// Organization represents a legal entity or business unit.
type Organization struct {
	entity.Catalog

	// FullName is the official full name of the organization
	FullName *string `db:"full_name" json:"fullName,omitempty"`

	// INN is the tax identification number
	INN *string `db:"inn" json:"inn,omitempty"`

	// KPP is the code of reason for registration
	KPP *string `db:"kpp" json:"kpp,omitempty"`

	// BaseCurrencyID is the main currency for accounting in this organization
	BaseCurrencyID id.ID `db:"base_currency_id" json:"baseCurrencyId,omitempty"`

	// IsDefault indicates if this is the default organization for new documents
	IsDefault bool `db:"is_default" json:"isDefault"`
}

// NewOrganization creates a new Organization with required fields.
func NewOrganization(code, name string, baseCurrencyID id.ID) *Organization {
	return &Organization{
		Catalog:        entity.NewCatalog(code, name),
		BaseCurrencyID: baseCurrencyID,
	}
}

// Validate implements entity.Validatable interface.
func (o *Organization) Validate(ctx context.Context) error {
	return o.Catalog.Validate(ctx)
}
