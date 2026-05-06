package content

import (
	"metapus/internal/metadata"
)

// BuildReportRegistry creates a minimal metadata.Registry containing only
// the entities referenced by report datasets. This enables the report Compiler
// in non-HTTP contexts (worker, scheduler) to resolve dereferenced field paths
// like "warehouse_id.name" → LEFT JOIN cat_warehouses.
//
// This is intentionally lightweight — it doesn't include all catalog metadata,
// only what's needed for JOIN resolution in the Query Engine.
func BuildReportRegistry() *metadata.Registry {
	reg := metadata.NewRegistry()

	// Warehouse — referenced by stock-balance, stock-turnover datasets.
	reg.RegisterReferenceMapping("warehouse", "Warehouse")
	reg.Register(metadata.EntityDef{
		Name:      "Warehouse",
		Key:       "warehouse",
		Type:      metadata.TypeCatalog,
		TableName: "cat_warehouses",
		Fields: []metadata.FieldDef{
			{Name: "name", Label: "Наименование", Type: metadata.TypeString},
			{Name: "code", Label: "Код", Type: metadata.TypeString},
		},
	})

	// Nomenclature — referenced by stock-balance, stock-turnover datasets.
	reg.RegisterReferenceMapping("nomenclature", "Nomenclature")
	reg.Register(metadata.EntityDef{
		Name:      "Nomenclature",
		Key:       "nomenclature",
		Type:      metadata.TypeCatalog,
		TableName: "cat_nomenclatures",
		Fields: []metadata.FieldDef{
			{Name: "name", Label: "Наименование", Type: metadata.TypeString},
			{Name: "code", Label: "Код", Type: metadata.TypeString},
		},
	})

	// Counterparty — referenced by document-journal dataset.
	reg.RegisterReferenceMapping("counterparty", "Counterparty")
	reg.Register(metadata.EntityDef{
		Name:      "Counterparty",
		Key:       "counterparty",
		Type:      metadata.TypeCatalog,
		TableName: "cat_counterparties",
		Fields: []metadata.FieldDef{
			{Name: "name", Label: "Наименование", Type: metadata.TypeString},
			{Name: "code", Label: "Код", Type: metadata.TypeString},
		},
	})

	// Organization — referenced by document-journal dataset.
	reg.RegisterReferenceMapping("organization", "Organization")
	reg.Register(metadata.EntityDef{
		Name:      "Organization",
		Key:       "organization",
		Type:      metadata.TypeCatalog,
		TableName: "cat_organizations",
		Fields: []metadata.FieldDef{
			{Name: "name", Label: "Наименование", Type: metadata.TypeString},
			{Name: "code", Label: "Код", Type: metadata.TypeString},
		},
	})

	return reg
}
