// Package export provides metadata-driven CSV and XLSX export for reports.
// Decoupled from domain/reports to avoid circular imports with compiler.
package export

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"

	"metapus/internal/platform"
)

// CSV writes report items as CSV to the given writer.
// Columns are determined by meta.Columns (visible only).
// Streams row-by-row — safe for any dataset size.
func CSV(w io.Writer, meta platform.ReportMeta, items []map[string]interface{}) error {
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

// XLSX creates an XLSX file from report items and writes to the writer.
func XLSX(w io.Writer, meta platform.ReportMeta, items []map[string]interface{}) (retErr error) {
	f := excelize.NewFile()
	defer func() {
		if cErr := f.Close(); cErr != nil && retErr == nil {
			retErr = fmt.Errorf("xlsx close: %w", cErr)
		}
	}()

	sheet := "Sheet1"
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
	if err := f.SetCellValue(sheet, "A1", meta.Name); err != nil {
		return fmt.Errorf("xlsx set title value: %w", err)
	}
	if err := f.SetCellStyle(sheet, "A1", "A1", titleStyle); err != nil {
		return fmt.Errorf("xlsx set title style: %w", err)
	}

	// ── Header row (row 3) ────────────────────────────────────────────
	headerRow := 3
	for i, col := range columns {
		cell := cellName(i, headerRow)
		if err := f.SetCellValue(sheet, cell, col.Label); err != nil {
			return fmt.Errorf("xlsx set header value: %w", err)
		}
		if err := f.SetCellStyle(sheet, cell, cell, headerStyle); err != nil {
			return fmt.Errorf("xlsx set header style: %w", err)
		}
		width := float64(len(col.Label)) * 1.3
		if width < 12 {
			width = 12
		}
		colLetter, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, colLetter, colLetter, width); err != nil {
			return fmt.Errorf("xlsx set col width: %w", err)
		}
	}

	// ── Data rows ─────────────────────────────────────────────────────
	for rowIdx, item := range items {
		row := headerRow + 1 + rowIdx
		for colIdx, col := range columns {
			cell := cellName(colIdx, row)
			val := item[col.Key]

			switch col.Type {
			case "quantity", "money":
				if num, ok := toFloat64(val); ok {
					if err := f.SetCellValue(sheet, cell, num); err != nil {
						return fmt.Errorf("xlsx set numeric value: %w", err)
					}
					if err := f.SetCellStyle(sheet, cell, cell, cellRightStyle); err != nil {
						return fmt.Errorf("xlsx set numeric style: %w", err)
					}
					continue
				}
			}

			if err := f.SetCellValue(sheet, cell, formatExportValue(val, col)); err != nil {
				return fmt.Errorf("xlsx set cell value: %w", err)
			}
			if err := f.SetCellStyle(sheet, cell, cell, cellStyle); err != nil {
				return fmt.Errorf("xlsx set cell style: %w", err)
			}
		}
	}

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
