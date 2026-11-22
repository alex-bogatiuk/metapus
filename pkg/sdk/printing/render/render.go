// Package render renders print forms to PDF and DOCX.
package render

import (
	"io"

	"metapus/internal/core/types"
	"metapus/internal/domain/printing"
	core "metapus/internal/infrastructure/printing"
)

type (
	// Renderer renders print form HTML using embedded Go templates.
	Renderer = core.Renderer
)

// FormatRawUnits formats an asset's raw units with the asset's own scale.
// The conversion is exact: no floating point and no assumed scale. Trailing
// fractional zeros beyond two digits (or the asset scale, if smaller) are
// dropped because they carry no value.
// Example: FormatRawUnits(1500000, 6) → "1,50"; (1234567, 6) → "1,234567"
func FormatRawUnits(v types.RawAmount, decimalPlaces int) string {
	return core.FormatRawUnits(v, decimalPlaces)
}

// NewRenderer loads all embedded templates and returns a ready Renderer.
func NewRenderer() (*core.Renderer, error) { return core.NewRenderer() }

// RenderDOCX writes the printmodel.PrintTable data as a Word .docx file to w.
func RenderDOCX(w io.Writer, data *printing.PrintData) error { return core.RenderDOCX(w, data) }
