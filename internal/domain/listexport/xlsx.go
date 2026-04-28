// Package listexport provides XLSX rendering for entity list export.
//
// Unlike reports/export (which handles grouping, subtotals, and datasets),
// this package renders a flat table from pre-mapped DTO rows.
// Uses excelize StreamWriter for O(1) memory consumption.
package listexport

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Column describes a single export column.
type Column struct {
	Key   string
	Label string
}

// XLSX writes a flat list as an Excel .xlsx file to w.
// title is rendered as a bold header row (e.g. entity plural name).
// columns defines the order and headers. rows contains DTO data as maps.
func XLSX(w io.Writer, title string, columns []Column, rows []map[string]any) (retErr error) {
	f := excelize.NewFile()
	defer func() {
		if cErr := f.Close(); cErr != nil && retErr == nil {
			retErr = fmt.Errorf("xlsx close: %w", cErr)
		}
	}()

	sheet := "Sheet1"

	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return fmt.Errorf("new stream writer: %w", err)
	}

	// ── Styles ────────────────────────────────────────────────────────
	titleStyleID, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 14},
	})
	headerStyleID, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E8E8E8"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    thinBorders(),
	})
	cellStyleID, _ := f.NewStyle(&excelize.Style{
		Font:   &excelize.Font{Size: 10},
		Border: thinBorders(),
	})
	cellRightStyleID, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10},
		Border:    thinBorders(),
		Alignment: &excelize.Alignment{Horizontal: "right"},
		NumFmt:    4, // #,##0.00
	})

	// ── Column widths ─────────────────────────────────────────────────
	for i, col := range columns {
		colLetter, _ := excelize.ColumnNumberToName(i + 1)
		width := float64(len(col.Label)) * 1.3
		if width < 14 {
			width = 14
		}
		if width > 50 {
			width = 50
		}
		if wErr := f.SetColWidth(sheet, colLetter, colLetter, width); wErr != nil {
			return fmt.Errorf("set col width %s: %w", colLetter, wErr)
		}
	}

	rowNum := 1

	// ── Title row ─────────────────────────────────────────────────────
	if err := sw.SetRow("A1", []interface{}{excelize.Cell{Value: title, StyleID: titleStyleID}}); err != nil {
		return fmt.Errorf("set title row: %w", err)
	}
	rowNum += 2 // blank row

	// ── Header row ────────────────────────────────────────────────────
	headerRow := make([]interface{}, len(columns))
	for i, col := range columns {
		headerRow[i] = excelize.Cell{Value: col.Label, StyleID: headerStyleID}
	}
	if err := sw.SetRow(fmt.Sprintf("A%d", rowNum), headerRow); err != nil {
		return fmt.Errorf("set header row: %w", err)
	}
	rowNum++

	// ── Data rows ─────────────────────────────────────────────────────
	for _, row := range rows {
		dataRow := make([]interface{}, len(columns))
		for i, col := range columns {
			val := row[col.Key]
			cell := formatCell(val, cellStyleID, cellRightStyleID)
			dataRow[i] = cell
		}
		if err := sw.SetRow(fmt.Sprintf("A%d", rowNum), dataRow); err != nil {
			return fmt.Errorf("set data row %d: %w", rowNum, err)
		}
		rowNum++
	}

	if err := sw.Flush(); err != nil {
		return fmt.Errorf("stream flush: %w", err)
	}

	return f.Write(w)
}

// formatCell converts a value to an excelize.Cell with appropriate style.
func formatCell(val any, textStyle, numStyle int) excelize.Cell {
	if val == nil {
		return excelize.Cell{Value: "", StyleID: textStyle}
	}
	switch v := val.(type) {
	case float64:
		return excelize.Cell{Value: v, StyleID: numStyle}
	case float32:
		return excelize.Cell{Value: float64(v), StyleID: numStyle}
	case int:
		return excelize.Cell{Value: v, StyleID: numStyle}
	case int64:
		return excelize.Cell{Value: v, StyleID: numStyle}
	case int32:
		return excelize.Cell{Value: int64(v), StyleID: numStyle}
	case bool:
		if v {
			return excelize.Cell{Value: "Да", StyleID: textStyle}
		}
		return excelize.Cell{Value: "Нет", StyleID: textStyle}
	case string:
		// Try to parse ISO date strings and format as DD.MM.YYYY HH:MM
		if len(v) >= 10 && (strings.Contains(v, "T") || strings.Count(v, "-") >= 2) {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return excelize.Cell{Value: t.Format("02.01.2006 15:04"), StyleID: textStyle}
			}
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return excelize.Cell{Value: t.Format("02.01.2006 15:04"), StyleID: textStyle}
			}
			// Date-only format
			if t, err := time.Parse("2006-01-02", v); err == nil {
				return excelize.Cell{Value: t.Format("02.01.2006"), StyleID: textStyle}
			}
		}
		return excelize.Cell{Value: v, StyleID: textStyle}
	default:
		return excelize.Cell{Value: fmt.Sprintf("%v", v), StyleID: textStyle}
	}
}

func thinBorders() []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: "CCCCCC", Style: 1},
		{Type: "top", Color: "CCCCCC", Style: 1},
		{Type: "right", Color: "CCCCCC", Style: 1},
		{Type: "bottom", Color: "CCCCCC", Style: 1},
	}
}
