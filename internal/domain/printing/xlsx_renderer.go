package printing

import (
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

// RenderXLSX writes the PrintTable data as an Excel .xlsx file to w.
func RenderXLSX(w io.Writer, data *PrintData) error {
	t := data.Table
	if t == nil {
		return fmt.Errorf("PrintData.Table is nil — XLSX export requires pre-formatted table data")
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := "Sheet1"

	// ── Styles ────────────────────────────────────────────────────────────
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	subtitleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "444444"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	headerLabelStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 9, Color: "555555"},
	})
	headerValueStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 10, Bold: true},
	})
	colHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 9},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F0F0F0"}},
		Border:    thinBorders(),
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})
	cellStyle, _ := f.NewStyle(&excelize.Style{
		Font:   &excelize.Font{Size: 10},
		Border: thinBorders(),
	})
	cellRightStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10},
		Border:    thinBorders(),
		Alignment: &excelize.Alignment{Horizontal: "right"},
	})
	totalLabelStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "444444"},
		Alignment: &excelize.Alignment{Horizontal: "right"},
	})
	totalValueStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Bold: true},
		Alignment: &excelize.Alignment{Horizontal: "right"},
		Border: []excelize.Border{
			{Type: "bottom", Color: "999999", Style: 1},
		},
	})
	totalGrandStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 12, Bold: true},
		Alignment: &excelize.Alignment{Horizontal: "right"},
		Border: []excelize.Border{
			{Type: "bottom", Color: "999999", Style: 2},
		},
	})

	row := 1
	numCols := len(t.Columns)
	if numCols == 0 {
		numCols = 1
	}
	lastCol := colName(numCols)

	// ── Title ─────────────────────────────────────────────────────────────
	_ = f.MergeCell(sheet, "A1", lastCol+"1")
	_ = f.SetCellValue(sheet, "A1", t.Title)
	_ = f.SetCellStyle(sheet, "A1", lastCol+"1", titleStyle)
	row++

	// ── Subtitle ──────────────────────────────────────────────────────────
	cell := fmt.Sprintf("A%d", row)
	endCell := fmt.Sprintf("%s%d", lastCol, row)
	_ = f.MergeCell(sheet, cell, endCell)
	_ = f.SetCellValue(sheet, cell, t.Subtitle)
	_ = f.SetCellStyle(sheet, cell, endCell, subtitleStyle)
	row += 2

	// ── Header fields ─────────────────────────────────────────────────────
	for _, headerRow := range t.HeaderRows {
		col := 1
		for _, hf := range headerRow {
			lCell := cellRef(col, row)
			_ = f.SetCellValue(sheet, lCell, hf.Label)
			_ = f.SetCellStyle(sheet, lCell, lCell, headerLabelStyle)
			col++
			vCell := cellRef(col, row)
			_ = f.SetCellValue(sheet, vCell, hf.Value)
			_ = f.SetCellStyle(sheet, vCell, vCell, headerValueStyle)
			col++ // gap column
			col++
		}
		row++
	}
	row++ // blank row before table

	// ── Column headers ────────────────────────────────────────────────────
	for i, colHeader := range t.Columns {
		c := cellRef(i+1, row)
		_ = f.SetCellValue(sheet, c, colHeader)
		_ = f.SetCellStyle(sheet, c, c, colHeaderStyle)
	}
	row++

	// ── Data rows ─────────────────────────────────────────────────────────
	for _, r := range t.Rows {
		for i, val := range r.Values {
			c := cellRef(i+1, row)
			_ = f.SetCellValue(sheet, c, val)
			// Right-align numeric columns (index >= 3 typically: qty, price, amount, vat)
			if i >= 3 {
				_ = f.SetCellStyle(sheet, c, c, cellRightStyle)
			} else {
				_ = f.SetCellStyle(sheet, c, c, cellStyle)
			}
		}
		row++
	}
	row++ // blank row before totals

	// ── Totals ────────────────────────────────────────────────────────────
	for _, total := range t.Totals {
		// Label in second-to-last column, value in last column
		labelCol := max(numCols-1, 1)
		valCol := numCols
		lc := cellRef(labelCol, row)
		vc := cellRef(valCol, row)
		_ = f.SetCellValue(sheet, lc, total.Label)
		_ = f.SetCellStyle(sheet, lc, lc, totalLabelStyle)
		_ = f.SetCellValue(sheet, vc, total.Value)
		if total.Grand {
			_ = f.SetCellStyle(sheet, vc, vc, totalGrandStyle)
		} else {
			_ = f.SetCellStyle(sheet, vc, vc, totalValueStyle)
		}
		row++
	}
	row += 2

	// ── Signatures (layout-driven from PrintSignatureBlock) ──────────────
	sigStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 9, Color: "444444"},
	})
	sigLineStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{Type: "bottom", Color: "666666", Style: 1},
		},
	})
	sigHintStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 8, Color: "999999"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})

	if sb := t.SignatureBlock; sb != nil && len(sb.Entries) > 0 {
		switch sb.Layout {
		case SignatureLayoutHorizontal:
			// All entries side-by-side across columns
			colsPerSig := max(numCols/len(sb.Entries), 2)

			// Row 1: role labels
			for i, entry := range sb.Entries {
				startCol := 1 + i*colsPerSig
				c := cellRef(startCol, row)
				_ = f.SetCellValue(sheet, c, entry.Role)
				_ = f.SetCellStyle(sheet, c, c, sigStyle)
			}
			row++

			// Row 2: underlines
			for i := range sb.Entries {
				startCol := 1 + i*colsPerSig
				endCol := startCol + colsPerSig - 1
				lineStart := cellRef(startCol, row)
				lineEnd := cellRef(endCol, row)
				_ = f.MergeCell(sheet, lineStart, lineEnd)
				_ = f.SetCellStyle(sheet, lineStart, lineEnd, sigLineStyle)
			}
			row++

			// Row 3: hints (from entry, not hardcoded)
			for i, entry := range sb.Entries {
				startCol := 1 + i*colsPerSig
				endCol := startCol + colsPerSig - 1
				hStart := cellRef(startCol, row)
				hEnd := cellRef(endCol, row)
				_ = f.MergeCell(sheet, hStart, hEnd)
				_ = f.SetCellValue(sheet, hStart, entry.Hint)
				_ = f.SetCellStyle(sheet, hStart, hEnd, sigHintStyle)
			}
			row += 2

		default: // vertical — each entry stacked
			for _, entry := range sb.Entries {
				c := cellRef(1, row)
				_ = f.SetCellValue(sheet, c, entry.Role)
				_ = f.SetCellStyle(sheet, c, c, sigStyle)
				row++
				lineStart := cellRef(1, row)
				lineEnd := cellRef(3, row)
				_ = f.MergeCell(sheet, lineStart, lineEnd)
				_ = f.SetCellStyle(sheet, lineStart, lineEnd, sigLineStyle)
				row++
				if entry.Hint != "" {
					hStart := cellRef(1, row)
					hEnd := cellRef(3, row)
					_ = f.MergeCell(sheet, hStart, hEnd)
					_ = f.SetCellValue(sheet, hStart, entry.Hint)
					_ = f.SetCellStyle(sheet, hStart, hEnd, sigHintStyle)
				}
				row += 2
			}
		}
	}

	// ── Column widths ─────────────────────────────────────────────────────
	colWidths := []float64{5, 30, 12, 12, 14, 14, 14}
	for i, w := range colWidths {
		if i < numCols {
			_ = f.SetColWidth(sheet, colName(i+1), colName(i+1), w)
		}
	}

	// ── Footer ────────────────────────────────────────────────────────────
	footerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 8, Color: "888888"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	footerCell := fmt.Sprintf("A%d", row)
	footerEnd := fmt.Sprintf("%s%d", lastCol, row)
	_ = f.MergeCell(sheet, footerCell, footerEnd)
	_ = f.SetCellValue(sheet, footerCell, "Сформировано автоматически системой Metapus")
	_ = f.SetCellStyle(sheet, footerCell, footerEnd, footerStyle)

	return f.Write(w)
}

// colName converts a 1-based column index to an Excel column letter (1→A, 2→B, ..., 27→AA).
func colName(n int) string {
	name := ""
	for n > 0 {
		n--
		name = string(rune('A'+n%26)) + name
		n /= 26
	}
	return name
}

// cellRef returns an Excel cell reference like "B5" for col=2, row=5.
func cellRef(col, row int) string {
	return fmt.Sprintf("%s%d", colName(col), row)
}

// thinBorders returns a thin border style for all 4 sides.
func thinBorders() []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: "CCCCCC", Style: 1},
		{Type: "right", Color: "CCCCCC", Style: 1},
		{Type: "top", Color: "CCCCCC", Style: 1},
		{Type: "bottom", Color: "CCCCCC", Style: 1},
	}
}
