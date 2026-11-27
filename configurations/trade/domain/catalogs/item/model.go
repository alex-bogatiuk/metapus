// Package item is the catalog of items (Номенклатура): goods and services
// by their article and manufacturer.
package item

import (
	"context"
	"strings"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
)

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "item", Name: "Item", Route: "items", Table: "cat_items",
	Kind: entity.CatalogKind,
	// An empty code is numbered from the catalog's series (IT-000001).
	Code:      entity.CodeRule{Prefix: "IT"},
	Hierarchy: entity.CatalogMeta{Hierarchical: true, HierarchyType: entity.HierarchyGroupsAndItems, FolderAsParentOnly: true},
}

// Item is one position of the catalog. Folders group positions into product
// categories (filters, brakes, suspension, ...).
type Item struct {
	entity.Catalog
	// Article is the manufacturer's part number (OEM article).
	Article      string `db:"article" json:"article" meta:"label:Артикул"`
	Manufacturer string `db:"manufacturer" json:"manufacturer" meta:"label:Производитель"`
	// Unit is the unit the stock is kept in (шт., л, комплект).
	Unit string `db:"unit" json:"unit" meta:"label:Единица измерения"`
}

func (Item) EntityDefinition() entity.Definition { return Definition }

func (e *Item) Validate(ctx context.Context) error {
	e.Article = strings.TrimSpace(e.Article)
	e.Manufacturer = strings.TrimSpace(e.Manufacturer)
	e.Unit = strings.TrimSpace(e.Unit)
	return e.Catalog.Validate(ctx)
}
