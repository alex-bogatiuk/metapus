package reports

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"

	"metapus/internal/platform"
)

// ---------------------------------------------------------------------------
// Report Export Engine
// ---------------------------------------------------------------------------
// Metadata-driven export: CSV and XLSX from ReportMeta + flat items.
// Streams rows directly to io.Writer (CSV) or builds excelize File (XLSX)
// to avoid OOM on large datasets.

// ExportCSV writes report items as CSV to the given writer.
// Columns are determined by meta.Columns (visible only).
// Streams row-by-row — safe for any dataset size.
func ExportCSV(w io.Writer, meta platform.ReportMeta, items []map[string]interface{}) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Visible columns only (exclude defaultHidden)
	columns := visibleColumns(meta)

	// Header row
	header := make([]string, len(columns))
	for i, col := range columns {
		header[i] = col.Label
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("csv write header: %w", err)
	}

	// Data rows
	row := make([]string, len(columns))
	for _, item := range items {
		for i, col := range columns {
			row[i] = formatExportValue(item[col.Key], col)
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("csv write row: %w", err)
		}
	}

	return nil
}

// ExportXLSX creates an XLSX file from report items and writes to the writer.
// Uses excelize (already a project dependency via printing/xlsx_renderer.go).
func ExportXLSX(w io.Writer, meta platform.ReportMeta, items []map[string]interface{}) error {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Sheet1"

	// Column definitions
	columns := visibleColumns(meta)

	// ── Styles ────────────────────────────────────────────────────────
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E8E8E8"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    thinBorders(),
	})
	cellStyle, _ := f.NewStyle(&excelize.Style{
		Font:   &excelize.Font{Size: 10},
		Border: thinBorders(),
	})
	cellRightStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10},
		Border:    thinBorders(),
		Alignment: &excelize.Alignment{Horizontal: "right"},
		NumFmt:    4, // #,##0.00
	})

	// ── Title row ─────────────────────────────────────────────────────
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 14},
	})
	f.SetCellValue(sheet, "A1", meta.Name)
	f.SetCellStyle(sheet, "A1", "A1", titleStyle)

	// ── Header row (row 3) ────────────────────────────────────────────
	headerRow := 3
	for i, col := range columns {
		cell := cellName(i, headerRow)
		f.SetCellValue(sheet, cell, col.Label)
		f.SetCellStyle(sheet, cell, cell, headerStyle)
		// Auto-width hint
		width := float64(len(col.Label)) * 1.3
		if width < 12 {
			width = 12
		}
		colLetter, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, colLetter, colLetter, width)
	}

	// ── Data rows ─────────────────────────────────────────────────────
	for rowIdx, item := range items {
		row := headerRow + 1 + rowIdx
		for colIdx, col := range columns {
			cell := cellName(colIdx, row)
			val := item[col.Key]

			// Write typed value for numeric columns
			switch col.Type {
			case "quantity", "money":
				if num, ok := toFloat64(val); ok {
					f.SetCellValue(sheet, cell, num)
					f.SetCellStyle(sheet, cell, cell, cellRightStyle)
					continue
				}
			}

			f.SetCellValue(sheet, cell, formatExportValue(val, col))
			f.SetCellStyle(sheet, cell, cell, cellStyle)
		}
	}

	// Write to output
	return f.Write(w)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func visibleColumns(meta platform.ReportMeta) []platform.ReportColumn {
	cols := make([]platform.ReportColumn, 0, len(meta.Columns))
	for _, c := range meta.Columns {
		if !c.DefaultHidden {
			cols = append(cols, c)
		}
	}
	return cols
}

func formatExportValue(v interface{}, col platform.ReportColumn) string {
	if v == nil {
		return ""
	}
	switch col.Type {
	case "quantity", "money":
		if num, ok := toFloat64(v); ok {
			return fmt.Sprintf("%.4f", num)
		}
	case "boolean":
		if b, ok := v.(bool); ok {
			if b {
				return "Да"
			}
			return "Нет"
		}
	}
	return fmt.Sprintf("%v", v)
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}

func cellName(col, row int) string {
	colLetter, _ := excelize.ColumnNumberToName(col + 1)
	return fmt.Sprintf("%s%d", colLetter, row)
}

func thinBorders() []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: "CCCCCC", Style: 1},
		{Type: "top", Color: "CCCCCC", Style: 1},
		{Type: "right", Color: "CCCCCC", Style: 1},
		{Type: "bottom", Color: "CCCCCC", Style: 1},
	}
}
