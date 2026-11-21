// Package warehouse is the catalog of the company's stock locations
// (Склады): the shop floor, the main warehouse, the defect warehouse, the
// service bay.
package warehouse

import (
	"context"

	"metapus/internal/core/entity"
)

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "warehouse", Name: "Warehouse", Route: "warehouses", Table: "cat_warehouses",
	Kind: entity.CatalogKind,
	// An empty code is numbered from the catalog's series (WH-000001).
	Code: entity.CodeRule{Prefix: "WH"},
}

// Warehouse is one stock location.
type Warehouse struct {
	entity.Catalog
	Address string `db:"address" json:"address" meta:"label:Адрес"`
}

func (Warehouse) EntityDefinition() entity.Definition { return Definition }

func (e *Warehouse) Validate(ctx context.Context) error { return e.Catalog.Validate(ctx) }
