// Package schema provides the declarative metadata types for the Metapus Query Engine.
//
// A Dataset describes one data source (table, CTE, or UNION) with its fields,
// dimensions, measures, and reference links. The Query Compiler uses Dataset
// definitions to dynamically build SQL queries based on user-selected fields.
//
// This is the "Schema Layer" — analogous to the Data Composition Schema (СКД)
// in 1C:Enterprise. Developers declare Datasets; users select fields at runtime.
package schema

// FieldKind classifies a field's role in the dataset.
type FieldKind string

const (
	// FieldDimension is a grouping/filterable field (e.g. warehouse, product).
	FieldDimension FieldKind = "dimension"
	// FieldMeasure is an aggregatable numeric field (e.g. quantity, amount).
	FieldMeasure FieldKind = "measure"
	// FieldAttribute is an informational field that is not aggregated (e.g. description).
	FieldAttribute FieldKind = "attribute"
)

// FieldType defines the data type of a dataset field.
type FieldType string

const (
	TypeString   FieldType = "string"
	TypeDate     FieldType = "date"
	TypeDatetime FieldType = "datetime"
	TypeQuantity FieldType = "quantity" // scaled numeric (types.Quantity)
	TypeMoney    FieldType = "money"    // scaled monetary (types.MinorUnits)
	TypeNumber   FieldType = "number"   // plain float
	TypeInteger  FieldType = "integer"
	TypeBoolean  FieldType = "boolean"
	TypeRef      FieldType = "ref" // FK reference to a catalog — enables auto-discovery
	TypeEnum     FieldType = "enum"
)

// AggFunc defines the aggregation function for measure fields.
type AggFunc string

const (
	AggSum   AggFunc = "sum"
	AggCount AggFunc = "count"
	AggAvg   AggFunc = "avg"
	AggMin   AggFunc = "min"
	AggMax   AggFunc = "max"
)

// Field describes a single field in a Dataset.
type Field struct {
	// Name is the SQL column name in the base table, e.g. "warehouse_id".
	Name string `json:"name"`

	// Label is the human-readable display name, e.g. "Склад".
	Label string `json:"label"`

	// Kind classifies the field as dimension, measure, or attribute.
	Kind FieldKind `json:"kind"`

	// Type defines the data type for rendering and validation.
	Type FieldType `json:"type"`

	// RefEntity is the entity key in metadata.Registry for Type==TypeRef.
	// E.g. "warehouse", "nomenclature". Used by Auto-Discovery to resolve
	// child fields and by Query Compiler to build LEFT JOINs.
	RefEntity string `json:"refEntity,omitempty"`

	// Agg is the aggregation function for Kind==FieldMeasure.
	// E.g. AggSum for "SUM(quantity)".
	Agg AggFunc `json:"aggFunc,omitempty"`

	// Hidden indicates the field is not shown by default in the UI.
	Hidden bool `json:"hidden,omitempty"`

	// Sortable indicates the field can be used for ORDER BY.
	Sortable bool `json:"sortable,omitempty"`

	// Scale is the number of decimal places for display (quantity=4, money=2).
	// Does NOT hardcode storage multiplier — that comes from types.QuantityScale.
	Scale int `json:"scale,omitempty"`

	// Alias overrides the output column name in QueryResult.
	// If empty, Name is used. Useful when two datasets have the same column name.
	Alias string `json:"alias,omitempty"`

	// FilterOnly marks a field that is used for filtering but never appears in SELECT.
	// E.g. "as_of_date" in StockBalance — a parameter, not a column.
	FilterOnly bool `json:"filterOnly,omitempty"`

	// EnumValues holds the allowed values for Type==TypeEnum.
	// Propagated from metadata.FieldDef.EnumValues during Auto-Discovery
	// so the frontend can render a <Select> dropdown instead of a text input.
	EnumValues []EnumValue `json:"enumValues,omitempty"`
}

// EnumValue represents a single enum option with backend value and display label.
// Mirrors metadata.EnumValue for cross-layer propagation.
type EnumValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Dataset describes a single data source for the Query Engine.
//
// Simple datasets (e.g. a flat table) set BaseTable and leave Executor nil.
// Complex datasets (CTE, UNION ALL, parametric subqueries) implement Executor
// and the Query Compiler calls Executor.BuildQuery() instead of selecting from BaseTable.
type Dataset struct {
	// Key is the unique identifier, e.g. "stock-balance". Used in API routes.
	Key string `json:"key"`

	// Name is the human-readable report title, e.g. "Остатки товаров".
	Name string `json:"name"`

	// Description is an optional longer description shown in the UI.
	Description string `json:"description,omitempty"`

	// BaseTable is the SQL table or view name, e.g. "reg_stock_movements".
	// Ignored when Executor is set.
	BaseTable string `json:"-"`

	// Permission is the required permission code, e.g. "report:stock:read".
	Permission string `json:"permission"`

	// Fields lists all available fields in this dataset.
	Fields []Field `json:"fields"`

	// Filters describes filter parameters that don't map to columns (e.g. date range).
	// These are passed to Executor.BuildQuery() as params but have no SELECT expression.
	Filters []FilterDef `json:"filters,omitempty"`

	// ScopeDimensions lists RLS dimension keys for DataScope checking.
	// E.g. ["warehouse"] means the user must have access to the warehouse dimension.
	ScopeDimensions []string `json:"scopeDimensions,omitempty"`

	// DefaultGroupBy lists field names that are grouped by default.
	DefaultGroupBy []string `json:"defaultGroupBy,omitempty"`

	// DefaultSort defines the default ORDER BY.
	DefaultSort *SortDef `json:"defaultSort,omitempty"`

	// ExportFormats lists supported export formats. Default: ["csv", "xlsx"].
	ExportFormats []string `json:"exportFormats,omitempty"`

	// Executor is an optional custom query builder for complex datasets.
	// If nil, Query Compiler generates a simple SELECT from BaseTable.
	Executor DatasetExecutor `json:"-"`
}

// FilterDef describes a dataset-level filter parameter (not a column).
// These are rendered as filter controls in the frontend.
type FilterDef struct {
	// Key is the parameter name sent from frontend, e.g. "as_of_date".
	Key string `json:"key"`

	// Label is the human-readable name, e.g. "Дата отчёта".
	Label string `json:"label"`

	// Type defines the filter control type.
	Type FilterType `json:"type"`

	// Required marks the filter as mandatory.
	Required bool `json:"required,omitempty"`

	// Ref is the entity key for Type==FilterRef (reference picker).
	Ref string `json:"ref,omitempty"`

	// Multi allows multiple values (e.g. multiple warehouse IDs).
	Multi bool `json:"multi,omitempty"`

	// Default is the default value for the filter.
	Default any `json:"default,omitempty"`
}

// FilterType defines the type of filter control rendered in the UI.
type FilterType string

const (
	FilterDate    FilterType = "date"
	FilterPeriod  FilterType = "period"
	FilterRef     FilterType = "reference"
	FilterBoolean FilterType = "boolean"
	FilterEnum    FilterType = "enum"
	FilterString  FilterType = "string"
)

// SortDef defines a default sort order.
type SortDef struct {
	Column    string `json:"column"`
	Direction string `json:"direction"` // "asc" or "desc"
}

// GetExportFormats returns the export formats, defaulting to csv+xlsx.
func (ds *Dataset) GetExportFormats() []string {
	if len(ds.ExportFormats) > 0 {
		return ds.ExportFormats
	}
	return []string{"csv", "xlsx"}
}

// FindField returns a field by name, or nil if not found.
func (ds *Dataset) FindField(name string) *Field {
	for i := range ds.Fields {
		if ds.Fields[i].Name == name {
			return &ds.Fields[i]
		}
	}
	return nil
}

// SelectableFields returns fields that are not filter-only.
func (ds *Dataset) SelectableFields() []Field {
	result := make([]Field, 0, len(ds.Fields))
	for _, f := range ds.Fields {
		if !f.FilterOnly {
			result = append(result, f)
		}
	}
	return result
}

// DefaultSelectedFields returns the names of fields that should be selected by default
// (non-hidden, non-filter-only).
// For ref fields (Type==TypeRef), automatically dereferences to ".name" so the user
// sees human-readable names (e.g. "Основной склад") instead of raw UUIDs.
func (ds *Dataset) DefaultSelectedFields() []string {
	result := make([]string, 0, len(ds.Fields))
	for _, f := range ds.Fields {
		if f.Hidden || f.FilterOnly {
			continue
		}
		if f.Type == TypeRef && f.RefEntity != "" {
			// Auto-dereference: warehouse_id → warehouse_id.name
			result = append(result, f.Name+".name")
		} else {
			result = append(result, f.OutputName())
		}
	}
	return result
}

// OutputName returns the display name for the field (alias if set, otherwise name).
func (f *Field) OutputName() string {
	if f.Alias != "" {
		return f.Alias
	}
	return f.Name
}
