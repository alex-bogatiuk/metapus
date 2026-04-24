// Package platform provides report contract types for metadata-driven reporting.
//
// ReportMeta, ReportColumn, etc. describe report structure declaratively.
// Used by both the Compiler (Dataset-based Query Engine) and the frontend.
package platform

// ---------------------------------------------------------------------------
// Report Metadata (returned by GET /reports/{key}/metadata)
// ---------------------------------------------------------------------------

// ReportMeta describes a report declaratively. The frontend uses this
// to auto-generate the filter sidebar, table columns, grouping controls,
// and totals footer without any report-specific UI code.
type ReportMeta struct {
	Key             string           `json:"key"`
	Name            string           `json:"name"`
	Description     string           `json:"description,omitempty"`
	Filters         []ReportFilter   `json:"filters"`
	Columns         []ReportColumn   `json:"columns"`
	GroupBy         []ReportGroupBy  `json:"groupBy,omitempty"`
	Totals          []ReportTotal    `json:"totals,omitempty"`
	ExportFormats   []string         `json:"exportFormats,omitempty"`
	ScopeDimensions []string         `json:"scopeDimensions,omitempty"`
	DefaultSort     *ReportSort      `json:"defaultSort,omitempty"`

	// AvailableFields is the auto-discovery tree of selectable fields.
	// Built by compiler.BuildFieldTree() from Dataset + metadata.Registry.
	// Root nodes are dataset fields; children are referenced entity attributes.
	AvailableFields []FieldTreeNode `json:"availableFields,omitempty"`
}

// FieldTreeNode describes a node in the field selection tree for the Report Builder UI.
// Root nodes represent dataset fields. For reference (ref) fields, Children contains
// the attributes of the referenced entity, resolved recursively up to MaxJoinDepth.
type FieldTreeNode struct {
	// Key is the full dot-separated path, e.g. "product_id.brand_id.name".
	Key string `json:"key"`
	// Name is the short field name, e.g. "name".
	Name string `json:"name"`
	// Label is the human-readable display name, e.g. "Наименование".
	Label string `json:"label"`
	// Type is the field data type: "string", "quantity", "ref", etc.
	Type string `json:"type"`
	// Kind is the field role: "dimension", "measure", "attribute".
	Kind string `json:"kind"`
	// Children are nested fields for ref-type nodes.
	Children []FieldTreeNode `json:"children,omitempty"`
	// Sortable indicates the field supports ORDER BY.
	Sortable bool `json:"sortable,omitempty"`
	// RefRoute is the route prefix for navigation (only for type="ref").
	RefRoute string `json:"refRoute,omitempty"`
}

// ReportFilter describes a single filter control in the sidebar.
type ReportFilter struct {
	Key      string `json:"key"`
	Type     string `json:"type"`               // "date", "period", "reference", "boolean", "enum", "string"
	Label    string `json:"label"`
	Required bool   `json:"required,omitempty"`
	Ref      string `json:"ref,omitempty"`       // entity name for reference picker (e.g. "warehouse")
	Multi    bool   `json:"multi,omitempty"`     // allow multiple values
	Default  any    `json:"default,omitempty"`   // default value
}

// ReportColumn describes a single column in the report table.
type ReportColumn struct {
	Key           string `json:"key"`
	Label         string `json:"label"`
	Type          string `json:"type"`                    // "string", "quantity", "money", "date", "reference", "boolean"
	Align         string `json:"align,omitempty"`         // "left", "center", "right"
	Sortable      bool   `json:"sortable,omitempty"`
	DefaultHidden bool   `json:"defaultHidden,omitempty"` // hidden by default, user can show via column chooser
	Format        string `json:"format,omitempty"`        // "number", "currency", "percent"
	// RefIdKey is the column key containing the raw UUID for reference columns.
	// E.g. for column "warehouse_id__name", RefIdKey = "warehouse_id".
	RefIdKey string `json:"refIdKey,omitempty"`
	// RefRoute is the frontend route prefix for navigating to the referenced entity.
	// E.g. "warehouses" → /catalogs/warehouses/{id}.
	RefRoute string `json:"refRoute,omitempty"`
}

// ReportGroupBy describes a grouping option available to the user.
type ReportGroupBy struct {
	Key           string `json:"key"`
	Label         string `json:"label"`
	DefaultActive bool   `json:"defaultActive,omitempty"` // active by default
}

// ReportTotal describes an aggregate total displayed in the footer.
type ReportTotal struct {
	Column string `json:"column"`              // which column to aggregate
	Func   string `json:"func"`                // "sum", "count", "avg", "min", "max"
	Label  string `json:"label,omitempty"`     // optional override label
}

// ReportSort describes the default sort order.
type ReportSort struct {
	Column    string `json:"column"`
	Direction string `json:"direction"` // "asc" or "desc"
}

