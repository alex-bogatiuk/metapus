package filter

// ComparisonType defines the comparison modes.
type ComparisonType string

const (
	Equal          ComparisonType = "eq"        // Equal
	NotEqual       ComparisonType = "neq"       // Not equal
	LessOrEqual    ComparisonType = "lte"       // Less or equal
	GreaterOrEqual ComparisonType = "gte"       // Greater or equal
	Less           ComparisonType = "lt"        // Less
	Greater        ComparisonType = "gt"        // Greater
	InList         ComparisonType = "in"        // In list
	NotInList      ComparisonType = "nin"       // Not in list
	Contains       ComparisonType = "contains"  // Contains (ILIKE %val%)
	NotContains    ComparisonType = "ncontains" // Does not contain (NOT ILIKE %val%)

	// Hierarchical filters
	InHierarchy    ComparisonType = "in_hierarchy"  // In hierarchy (in group or subgroups)
	NotInHierarchy ComparisonType = "nin_hierarchy" // Not in hierarchy

	// Additional
	IsNull    ComparisonType = "null"     // Not filled (null)
	IsNotNull ComparisonType = "not_null" // Filled (not null)
)

// SearchFieldName is the special pseudo-field name used by the frontend
// to pass a fuzzy search query. It is NOT a real column — it must be
// intercepted before reaching BuildConditions and handled via BuildSearchConditions.
const SearchFieldName = "__search"

// Item represents a single filter condition.
type Item struct {
	Field     string         `json:"field"`               // Field name (snake_case)
	FieldType string         `json:"fieldType,omitempty"` // Field type (e.g., date)
	Operator  ComparisonType `json:"operator"`            // Comparison mode
	Value     any            `json:"value"`               // Value (string, number, array of IDs)
	Scale     int            `json:"scale,omitempty"`     // Storage multiplier (e.g. 10000 for Quantity, 100 for Money)
}

// TablePartInfo describes a child table (table part / tabular section)
// for generating EXISTS subqueries when filtering by table part columns.
type TablePartInfo struct {
	TableName  string              // SQL table name, e.g. "doc_goods_receipt_lines"
	ForeignKey string              // FK column linking to parent, e.g. "document_id"
	ValidCols  map[string]struct{} // allowed filter columns in this table part
}

// ReferenceFieldInfo describes a linked reference entity (catalog)
// for generating EXISTS subqueries when filtering by nested reference fields.
type ReferenceFieldInfo struct {
	TableName  string              // SQL table name of the reference entity, e.g. "cat_counterparties"
	ForeignKey string              // FK column in the parent entity linking to the reference, e.g. "counterparty_id"
	ValidCols  map[string]struct{} // allowed filter columns in the reference entity
}
