// Package platform provides report contract types for metadata-driven reporting.
//
// ReportRegistration[F, R] is the typed contract for reports.
// F is the filter type (parsed from query params), R is the result type.
//
// The platform automatically:
//   - creates GET /{prefix} endpoint → Execute()
//   - creates GET /{prefix}/metadata endpoint → Meta()
//   - applies RequirePermission(Permission())
//   - applies DataScope (RLS) from ScopeDimensions
//
// Usage:
//
//	type StockBalanceReport struct{ repo Repository }
//	func (r *StockBalanceReport) RoutePrefix() string { return "stock-balance" }
//	func (r *StockBalanceReport) Permission() string  { return "report:stock:read" }
//	func (r *StockBalanceReport) Meta() platform.ReportMeta { ... }
//	func (r *StockBalanceReport) Execute(ctx context.Context, f Filter) (Result, error) { ... }
package platform

import "context"

// ---------------------------------------------------------------------------
// Report Contract (Generic)
// ---------------------------------------------------------------------------

// ReportRegistration is the typed interface for metadata-driven reports.
// Each report implements this with its own Filter and Result types.
//
// The generic handler (report_handler.go) uses reflection to parse
// query parameters into F and serializes R as JSON.
type ReportRegistration[F any, R any] interface {
	// RoutePrefix returns the URL path segment, e.g. "stock-balance".
	RoutePrefix() string
	// Permission returns the required permission code, e.g. "report:stock:read".
	Permission() string
	// Meta returns the declarative metadata used by the frontend to build
	// filters, columns, grouping controls, and totals automatically.
	Meta() ReportMeta
	// Execute runs the report with the given filter and returns the result.
	Execute(ctx context.Context, filter F) (R, error)
}

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

// ---------------------------------------------------------------------------
// Type-Erased Wrapper (bridges generic → non-generic FactoryRegistry)
// ---------------------------------------------------------------------------

// ReportRouteAdapter is the non-generic interface stored in FactoryRegistry.
// It wraps a typed ReportRegistration[F, R] and exposes metadata + handler builders.
type ReportRouteAdapter interface {
	// RoutePrefix returns the URL segment.
	RoutePrefix() string
	// Permission returns the required permission code.
	Permission() string
	// Meta returns the report metadata.
	Meta() ReportMeta
	// HandleExecute is the gin.HandlerFunc for GET /{prefix}
	HandleExecute(ctx context.Context, queryBinder func(dst any) error) (any, error)
}
