package entity

import (
	"context"

	"metapus/internal/core/apperror"
)

// Catalog is the base type for reference data (Справочники).
// Examples: Nomenclature, Counterparties, Warehouses, Organizations.
type Catalog struct {
	BaseCatalog

	// Code is a human-readable identifier (unique within tenant database)
	Code string `db:"code" json:"code"`

	// Name is the display name
	Name string `db:"name" json:"name"`

	// ParentID for hierarchical catalogs (nullable)
	ParentID *string `db:"parent_id" json:"parentId,omitempty"`

	// IsFolder indicates if this is a group (folder) in hierarchy
	IsFolder bool `db:"is_folder" json:"isFolder"`
}

// NewCatalog creates a new Catalog with generated ID.
// In Database-per-Tenant architecture, tenantID is not required.
func NewCatalog(code, name string) Catalog {
	return Catalog{
		BaseCatalog: NewBaseCatalog(),
		Code:        code,
		Name:        name,
	}
}

// Validate implements Validatable interface.
func (c *Catalog) Validate(ctx context.Context) error {
	if c.Name == "" {
		return apperror.NewValidation("name is required").
			WithDetail("field", "name")
	}

	// Code can be auto-generated, so it's optional at creation
	// but required at save time

	return nil
}

// SetParent sets the parent reference.
func (c *Catalog) SetParent(parentID string) {
	if parentID == "" {
		c.ParentID = nil
	} else {
		c.ParentID = &parentID
	}
}

// IsRoot returns true if catalog has no parent.
func (c *Catalog) IsRoot() bool {
	return c.ParentID == nil || *c.ParentID == ""
}
