// Package printing declares print forms of documents.
package printing

import (
	core "metapus/internal/domain/printing"
)

type (
	// PDFRenderer converts self-contained HTML without accepting a navigation URL.
	// Browser execution belongs to the infrastructure service, outside the ERP server.
	PDFRenderer = core.PDFRenderer
	// PrintData is the context passed to every print template.
	PrintData = core.PrintData
	// PrintFormDef defines a single print form for a document type.
	PrintFormDef = core.PrintFormDef
	// PrintFormRegistry stores available print forms per document type.
	// Follows the same Abstract Factory pattern as catalogFactories / documentFactories.
	PrintFormRegistry = core.PrintFormRegistry
	// PrintHeaderField is a label-value pair in the document header area.
	PrintHeaderField = core.PrintHeaderField
	// PrintTable is a format-agnostic representation of a document print form.
	// HTML renderer uses PrintData.Doc + Go templates; XLSX/DOCX renderers use this flat structure.
	// All layout decisions (signature orientation, header grid, etc.) are expressed here —
	// renderers must NOT hardcode layout, they interpret this struct.
	PrintTable = core.PrintTable
	// PrintTableRow is a single line item row with pre-formatted cell values.
	PrintTableRow = core.PrintTableRow
	// PrintTotalLine is a label-value pair in the totals section.
	PrintTotalLine = core.PrintTotalLine
)

const (
	// CategoryStandard marks a built-in (platform) print form.
	CategoryStandard = core.CategoryStandard
	// TableTemplate is the platform template that renders the form label and
	// PrintData.Table on an A4 page.
	TableTemplate = core.TableTemplate
)

// NewPrintFormRegistry creates an empty registry. Document registrations
// declare their forms through v1.PrintFormsProvider.
func NewPrintFormRegistry() *core.PrintFormRegistry { return core.NewPrintFormRegistry() }
