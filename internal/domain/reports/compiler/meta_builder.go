package compiler

import (
	"metapus/internal/domain/reports/schema"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

// DatasetToMeta converts a schema.Dataset into a platform.ReportMeta
// suitable for the frontend. Also builds the AvailableFields tree
// via Auto-Discovery from the metadata registry.
//
// This is the bridge between the new declarative Dataset definitions
// and the existing frontend contract (useReportPage.ts reads ReportMeta).
func DatasetToMeta(ds *schema.Dataset, reg *metadata.Registry) platform.ReportMeta {
	meta := platform.ReportMeta{
		Key:             ds.Key,
		Name:            ds.Name,
		Description:     ds.Description,
		ExportFormats:   ds.GetExportFormats(),
		ScopeDimensions: ds.ScopeDimensions,
	}

	// Build columns from fields
	columns := make([]platform.ReportColumn, 0, len(ds.Fields))
	for _, f := range ds.Fields {
		if f.FilterOnly {
			continue
		}

		key := f.OutputName()
		colType := fieldTypeToColumnType(f.Type)

		// For ref fields, the default SELECT auto-dereferences to ".name",
		// producing SQL alias "warehouse_id__name". Match the column key to that.
		if f.Type == schema.TypeRef && f.RefEntity != "" {
			key = f.Name + "__name"
			colType = "string" // the dereferenced value is a string (name)
		}

		col := platform.ReportColumn{
			Key:           key,
			Label:         f.Label,
			Type:          colType,
			Sortable:      f.Sortable,
			DefaultHidden: f.Hidden,
		}
		if f.Type == schema.TypeQuantity || f.Type == schema.TypeMoney || f.Type == schema.TypeNumber {
			col.Align = "right"
		}
		columns = append(columns, col)
	}
	meta.Columns = columns

	// Build filters from dataset-level FilterDefs
	filters := make([]platform.ReportFilter, 0, len(ds.Filters))
	for _, fd := range ds.Filters {
		filters = append(filters, platform.ReportFilter{
			Key:      fd.Key,
			Type:     string(fd.Type),
			Label:    fd.Label,
			Required: fd.Required,
			Ref:      fd.Ref,
			Multi:    fd.Multi,
			Default:  fd.Default,
		})
	}
	// Also add Ref-typed dimension fields as reference filters
	for _, f := range ds.Fields {
		if f.Kind == schema.FieldDimension && f.Type == schema.TypeRef && f.RefEntity != "" {
			filters = append(filters, platform.ReportFilter{
				Key:   f.Name,
				Type:  "reference",
				Label: f.Label,
				Ref:   f.RefEntity,
				Multi: true,
			})
		}
	}
	meta.Filters = filters

	// Build groupBy from dimension fields
	groupBy := make([]platform.ReportGroupBy, 0)
	for _, f := range ds.Fields {
		if f.Kind == schema.FieldDimension && !f.FilterOnly {
			isDefault := false
			for _, dg := range ds.DefaultGroupBy {
				if dg == f.OutputName() {
					isDefault = true
					break
				}
			}
			groupBy = append(groupBy, platform.ReportGroupBy{
				Key:           f.OutputName(),
				Label:         f.Label,
				DefaultActive: isDefault,
			})
		}
	}
	meta.GroupBy = groupBy

	// Build totals from measure fields
	totals := make([]platform.ReportTotal, 0)
	for _, f := range ds.Fields {
		if f.Kind == schema.FieldMeasure && f.Agg != "" {
			totals = append(totals, platform.ReportTotal{
				Column: f.OutputName(),
				Func:   string(f.Agg),
			})
		}
	}
	meta.Totals = totals

	// Default sort
	if ds.DefaultSort != nil {
		meta.DefaultSort = &platform.ReportSort{
			Column:    ds.DefaultSort.Column,
			Direction: ds.DefaultSort.Direction,
		}
	}

	// Auto-Discovery: build field tree
	meta.AvailableFields = BuildFieldTree(ds, reg, MaxJoinDepth)

	return meta
}

// fieldTypeToColumnType maps schema.FieldType → platform.ReportColumn.Type.
func fieldTypeToColumnType(ft schema.FieldType) string {
	switch ft {
	case schema.TypeQuantity:
		return "quantity"
	case schema.TypeMoney:
		return "money"
	case schema.TypeDate, schema.TypeDatetime:
		return "date"
	case schema.TypeBoolean:
		return "boolean"
	case schema.TypeRef:
		return "reference"
	default:
		return "string"
	}
}
