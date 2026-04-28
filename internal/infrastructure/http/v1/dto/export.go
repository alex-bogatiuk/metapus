package dto

import "encoding/json"

// ExportListRequest is the DTO for universal list export to XLSX.
// Frontend sends the visible columns (in display order) along with the same
// filter parameters used by the List endpoint.
type ExportListRequest struct {
	Columns        []ExportColumn  `json:"columns" binding:"required,min=1,max=100"`
	Filter         json.RawMessage `json:"filter"`
	OrderBy        string          `json:"orderBy"`
	IncludeDeleted bool            `json:"includeDeleted"`
	Search         string          `json:"search"`
}

// ExportColumn describes a single column to include in the export.
type ExportColumn struct {
	Key   string `json:"key" binding:"required"`
	Label string `json:"label" binding:"required"`
}

// ExportTablePartRequest is the DTO for exporting a document table part to XLSX.
// Frontend sends pre-resolved rows (human-readable names, scaled amounts) —
// the backend only renders XLSX, no data fetching or transformation needed.
type ExportTablePartRequest struct {
	// Title of the table part sheet (e.g. "Товары")
	Title string `json:"title" binding:"required"`
	// DocumentTitle for the XLSX title row (e.g. "Поступление GR-0137 от 28.04.2026")
	DocumentTitle string `json:"documentTitle" binding:"required"`
	// Columns in display order
	Columns []ExportColumn `json:"columns" binding:"required,min=1,max=100"`
	// Rows — pre-resolved, human-readable data from frontend state
	Rows []map[string]any `json:"rows" binding:"required"`
}
