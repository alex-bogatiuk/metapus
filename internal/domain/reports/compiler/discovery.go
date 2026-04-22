package compiler

import (
	"metapus/internal/domain/reports/schema"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

// BuildFieldTree builds the tree of available fields for a dataset.
// For each Ref-typed field, it recursively resolves child fields from the
// referenced entity via metadata.Registry, up to maxDepth levels.
//
// Example output for StockBalance dataset:
//
//	warehouse_id (Склад) [ref]
//	├── name (Наименование) [string]
//	├── code (Код) [string]
//	└── ...
//	product_id (Товар) [ref]
//	├── name (Наименование) [string]
//	├── article (Артикул) [string]
//	├── brand_id (Бренд) [ref]
//	│   ├── name (Наименование) [string]
//	│   └── country_id (Страна) [ref] ← depth=3, STOP
//	└── base_unit_id (Ед. изм.) [ref]
//	    └── name (Наименование) [string]
//	quantity (Остаток) [measure]
func BuildFieldTree(ds *schema.Dataset, reg *metadata.Registry, maxDepth int) []platform.FieldTreeNode {
	if maxDepth <= 0 {
		maxDepth = MaxJoinDepth
	}

	nodes := make([]platform.FieldTreeNode, 0, len(ds.Fields))
	for _, f := range ds.Fields {
		if f.FilterOnly {
			continue
		}
		node := fieldToTreeNode(f, "", reg, maxDepth, 0)
		nodes = append(nodes, node)
	}
	return nodes
}

// fieldToTreeNode converts a schema.Field into a FieldTreeNode,
// recursively resolving children for ref-type fields.
func fieldToTreeNode(f schema.Field, prefix string, reg *metadata.Registry, maxDepth, currentDepth int) platform.FieldTreeNode {
	key := f.OutputName()
	if prefix != "" {
		key = prefix + "." + f.OutputName()
	}

	node := platform.FieldTreeNode{
		Key:      key,
		Name:     f.OutputName(),
		Label:    f.Label,
		Type:     string(f.Type),
		Kind:     string(f.Kind),
		Sortable: f.Sortable,
	}

	// Recursively resolve ref fields
	if f.Type == schema.TypeRef && f.RefEntity != "" && currentDepth < maxDepth {
		children := resolveRefChildren(f.RefEntity, key, reg, maxDepth, currentDepth+1)
		if len(children) > 0 {
			node.Children = children
		}
	}

	return node
}

// resolveRefChildren looks up a referenced entity in the registry and
// converts its fields into child FieldTreeNodes.
func resolveRefChildren(refEntity, parentKey string, reg *metadata.Registry, maxDepth, currentDepth int) []platform.FieldTreeNode {
	if reg == nil {
		return nil
	}

	// Resolve entity: first via reference mapping, then by direct name
	entityName, ok := reg.GetEntityByRefType(refEntity)
	if !ok {
		entityName = refEntity
	}

	entityDef, ok := reg.Get(entityName)
	if !ok {
		return nil
	}

	// Skip system/audit/internal fields not useful in reports
	skipFields := map[string]bool{
		"id": true, "version": true, "attributes": true,
		"createdAt": true, "updatedAt": true,
		"createdBy": true, "updatedBy": true,
		"txid": true, "deletedAt": true,
		"deletionMark": true, "parentId": true, "isFolder": true,
		"imageUrl": true,
	}

	children := make([]platform.FieldTreeNode, 0, len(entityDef.Fields))
	for _, ef := range entityDef.Fields {
		if skipFields[ef.Name] {
			continue
		}

		childField := metaFieldToSchemaField(ef)
		child := fieldToTreeNode(childField, parentKey, reg, maxDepth, currentDepth)
		children = append(children, child)
	}
	return children
}

// metaFieldToSchemaField converts a metadata.FieldDef to a schema.Field
// for use in tree node generation.
func metaFieldToSchemaField(mf metadata.FieldDef) schema.Field {
	f := schema.Field{
		Name:     mf.Name,
		Label:    mf.Label,
		Kind:     schema.FieldAttribute,
		Sortable: true,
	}

	// Map metadata field type to schema field type
	switch mf.Type {
	case metadata.TypeString, metadata.TypeText:
		f.Type = schema.TypeString
	case metadata.TypeInteger:
		f.Type = schema.TypeInteger
	case metadata.TypeNumber, metadata.TypeDecimal:
		f.Type = schema.TypeNumber
		f.Scale = mf.Scale
	case metadata.TypeMoney:
		f.Type = schema.TypeMoney
	case metadata.TypeBoolean:
		f.Type = schema.TypeBoolean
	case metadata.TypeDate, metadata.TypeDatetime:
		f.Type = schema.TypeDate
	case metadata.TypeReference:
		f.Type = schema.TypeRef
		f.RefEntity = mf.ReferenceType
	case metadata.TypeEnum:
		f.Type = schema.TypeEnum
	default:
		f.Type = schema.TypeString
	}

	return f
}
