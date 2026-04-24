// Package export provides metadata-driven CSV and XLSX export for reports.
// Decoupled from domain/reports to avoid circular imports with compiler.
package export

import (
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"metapus/internal/platform"
)



// XLSX creates an XLSX file from report items and writes to the writer.
// If exportColumnKeys is provided, columns are output in that order.
// Uses a StreamWriter for O(1) memory consumption and supports Control Breaks (Subtotals).
func XLSX(w io.Writer, meta platform.ReportMeta, items []map[string]interface{}, exportColumnKeys []string, exportGroupByKeys []string) (retErr error) {
	f := excelize.NewFile()
	defer func() {
		if cErr := f.Close(); cErr != nil && retErr == nil {
			retErr = fmt.Errorf("xlsx close: %w", cErr)
		}
	}()

	sheet := "Sheet1"
	columns := resolveExportColumns(meta, exportColumnKeys)

	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return fmt.Errorf("new stream writer: %w", err)
	}

	// ── Styles ────────────────────────────────────────────────────────
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
	titleStyleID, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 14},
	})
	groupHeaderStyleID, _ := f.NewStyle(&excelize.Style{
		Font:   &excelize.Font{Bold: true, Size: 10},
		Fill:   excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F5F5F5"}},
		Border: thinBorders(),
	})
	subtotalStyleID, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F5F5F5"}},
		Alignment: &excelize.Alignment{Horizontal: "right"},
		Border:    thinBorders(),
	})
	subtotalNumStyleID, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F5F5F5"}},
		Alignment: &excelize.Alignment{Horizontal: "right"},
		NumFmt:    4,
		Border:    thinBorders(),
	})

	// Pre-calc numeric cols
	numericCols := make([]bool, len(columns))
	for i, col := range columns {
		if col.Type == "quantity" || col.Type == "money" || col.Type == "number" {
			numericCols[i] = true
		}
	}

	rowNum := 1

	// Title
	sw.SetRow("A1", []interface{}{excelize.Cell{Value: meta.Name, StyleID: titleStyleID}})
	rowNum += 2 // Blank row

	// Header & Column Widths
	headerRow := make([]interface{}, len(columns))
	for i, col := range columns {
		headerRow[i] = excelize.Cell{Value: col.Label, StyleID: headerStyleID}
		colLetter, _ := excelize.ColumnNumberToName(i + 1)
		width := float64(len(col.Label)) * 1.3
		if width < 12 {
			width = 12
		}
		f.SetColWidth(sheet, colLetter, colLetter, width)
	}
	sw.SetRow(fmt.Sprintf("A%d", rowNum), headerRow)
	rowNum++

	// ── Grouping & Control Breaks ─────────────────────────────────────
	type groupState struct {
		Key       string
		ColMeta   platform.ReportColumn
		Value     string
		Subtotals []float64
	}
	var groups []*groupState
	for _, key := range exportGroupByKeys {
		groups = append(groups, &groupState{
			Key:       key,
			ColMeta:   getColMeta(meta, key),
			Value:     "",
			Subtotals: make([]float64, len(columns)),
		})
	}
	grandTotals := make([]float64, len(columns))
	firstRow := true

	for _, item := range items {
		// 1. Detect break
		breakIdx := -1
		if !firstRow {
			for i, g := range groups {
				val := formatExportValue(item[g.Key], g.ColMeta)
				if val != g.Value {
					breakIdx = i
					break
				}
			}
		}

		// 2. Output Subtotals (bottom-up)
		if breakIdx != -1 {
			for i := len(groups) - 1; i >= breakIdx; i-- {
				g := groups[i]
				subRow := make([]interface{}, len(columns))
				subRow[0] = excelize.Cell{Value: fmt.Sprintf("Итого %s", g.Value), StyleID: subtotalStyleID}
				for cIdx, isNum := range numericCols {
					if isNum {
						subRow[cIdx] = excelize.Cell{Value: g.Subtotals[cIdx], StyleID: subtotalNumStyleID}
					}
					g.Subtotals[cIdx] = 0 // Reset
				}
				sw.SetRow(fmt.Sprintf("A%d", rowNum), subRow, excelize.RowOpts{OutlineLevel: i})
				rowNum++
			}
		}

		// 3. Update group values & Output new Headers (top-down)
		if firstRow || breakIdx != -1 {
			startIdx := 0
			if !firstRow {
				startIdx = breakIdx
			}
			for i := startIdx; i < len(groups); i++ {
				g := groups[i]
				val := formatExportValue(item[g.Key], g.ColMeta)
				g.Value = val

				hdrRow := make([]interface{}, len(columns))
				indent := strings.Repeat("   ", i)
				hdrRow[0] = excelize.Cell{Value: indent + val, StyleID: groupHeaderStyleID}
				sw.SetRow(fmt.Sprintf("A%d", rowNum), hdrRow, excelize.RowOpts{OutlineLevel: i})
				rowNum++
			}
			firstRow = false
		}

		// 4. Detail Row
		detRow := make([]interface{}, len(columns))
		for cIdx, col := range columns {
			val := item[col.Key]
			if numericCols[cIdx] {
				if num, ok := toFloat64(val); ok {
					detRow[cIdx] = excelize.Cell{Value: num, StyleID: cellRightStyleID}
					for _, g := range groups {
						g.Subtotals[cIdx] += num
					}
					grandTotals[cIdx] += num
				}
			} else {
				detRow[cIdx] = excelize.Cell{Value: formatExportValue(val, col), StyleID: cellStyleID}
			}
		}
		sw.SetRow(fmt.Sprintf("A%d", rowNum), detRow, excelize.RowOpts{OutlineLevel: len(groups)})
		rowNum++
	}

	// 5. Remaining Subtotals (if there were items)
	if !firstRow {
		for i := len(groups) - 1; i >= 0; i-- {
			g := groups[i]
			subRow := make([]interface{}, len(columns))
			subRow[0] = excelize.Cell{Value: fmt.Sprintf("Итого %s", g.Value), StyleID: subtotalStyleID}
			for cIdx, isNum := range numericCols {
				if isNum {
					subRow[cIdx] = excelize.Cell{Value: g.Subtotals[cIdx], StyleID: subtotalNumStyleID}
				}
			}
			sw.SetRow(fmt.Sprintf("A%d", rowNum), subRow, excelize.RowOpts{OutlineLevel: i})
			rowNum++
		}
	}

	// 6. Grand Totals
	if len(items) > 0 {
		gtRow := make([]interface{}, len(columns))
		gtRow[0] = excelize.Cell{Value: "Итого по отчету", StyleID: subtotalStyleID}
		for cIdx, isNum := range numericCols {
			if isNum {
				gtRow[cIdx] = excelize.Cell{Value: grandTotals[cIdx], StyleID: subtotalNumStyleID}
			}
		}
		sw.SetRow(fmt.Sprintf("A%d", rowNum), gtRow)
	}

	if err := sw.Flush(); err != nil {
		return fmt.Errorf("stream flush: %w", err)
	}

	return f.Write(w)
}

func getColMeta(meta platform.ReportMeta, key string) platform.ReportColumn {
	for _, c := range meta.Columns {
		if c.Key == key {
			return c
		}
	}
	return platform.ReportColumn{Key: key, Type: "string", Label: key}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveExportColumns returns columns in the order specified by exportColumnKeys.
// If exportColumnKeys is empty, falls back to all visible (non-hidden) columns.
// Unknown keys are silently skipped.
func resolveExportColumns(meta platform.ReportMeta, exportColumnKeys []string) []platform.ReportColumn {
	if len(exportColumnKeys) == 0 {
		return visibleColumns(meta)
	}

	// Build lookup map: key → column
	colMap := make(map[string]platform.ReportColumn, len(meta.Columns))
	for _, c := range meta.Columns {
		colMap[c.Key] = c
	}

	// Resolve in user's order
	cols := make([]platform.ReportColumn, 0, len(exportColumnKeys))
	for _, key := range exportColumnKeys {
		if col, ok := colMap[key]; ok {
			cols = append(cols, col)
		}
	}

	if len(cols) == 0 {
		return visibleColumns(meta)
	}
	return cols
}

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
