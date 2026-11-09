// Package export provides metadata-driven CSV and XLSX export for reports.
// Decoupled from domain/reports to avoid circular imports with compiler.
package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	reportcontract "metapus/internal/domain/reports/contract"
	"metapus/internal/domain/reports/tabular"
)

// XLSX creates an XLSX file from report items and writes to the writer.
// If exportColumnKeys is provided, columns are output in that order.
// Uses a StreamWriter for O(1) memory consumption and supports Control Breaks (Subtotals).
func XLSX(w io.Writer, meta reportcontract.ReportMeta, items []map[string]any, exportColumnKeys []string, exportGroupByKeys []string) (retErr error) {
	columns := resolveExportColumns(meta, exportColumnKeys)
	rows, err := tabular.BuildDisplayRows(items, exportGroupByKeys, meta.Totals)
	if err != nil {
		return err
	}
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); retErr == nil {
			retErr = err
		}
	}()
	sw, err := f.NewStreamWriter("Sheet1")
	if err != nil {
		return err
	}
	styles := map[string]int{}
	style := func(bold bool, format string) (int, error) {
		key := fmt.Sprintf("%t:%s", bold, format)
		if id, ok := styles[key]; ok {
			return id, nil
		}
		options := &excelize.Style{Font: &excelize.Font{Bold: bold, Size: 10}, Border: thinBorders()}
		if format != "" {
			options.CustomNumFmt = &format
			options.Alignment = &excelize.Alignment{Horizontal: "right", WrapText: true}
		}
		id, err := f.NewStyle(options)
		if err == nil {
			styles[key] = id
		}
		return id, err
	}
	headerStyle, err := style(true, "")
	if err != nil {
		return err
	}
	header := make([]any, len(columns))
	for i, col := range columns {
		header[i] = excelize.Cell{Value: col.Label, StyleID: headerStyle}
		if err = sw.SetColWidth(i+1, i+1, 24); err != nil {
			return err
		}
	}
	if err = sw.SetRow("A1", []any{excelize.Cell{Value: meta.Name, StyleID: headerStyle}}); err != nil {
		return err
	}
	if err = sw.SetRow("A3", header); err != nil {
		return err
	}
	for i, row := range rows {
		cells := make([]any, len(columns))
		labelWritten := false
		for j, col := range columns {
			var value any
			format := ""
			switch row.Kind {
			case "data":
				if row.Item[col.Key] == nil {
					break
				}
				switch col.Type {
				case "money":
					unit, err := tabular.ReadMoneyUnit(row.Item, col.Money)
					if err != nil {
						return err
					}
					exact, err := tabular.FormatMoneyFor(row.Item[col.Key], unit, col.Money)
					if err != nil {
						return err
					}
					value, err = tabular.ExcelDecimal(exact)
					if err != nil {
						return err
					}
					format = moneyFormat(unit.DecimalPlaces)
				case "quantity", "number":
					value = row.Item[col.Key]
					format = "#,##0.####"
				default:
					value, err = tabular.FormatCell(row.Item, col)
					if err != nil {
						return err
					}
				}
			case "group":
				if j == 0 {
					value = row.Label
				}
			default:
				total, exists := row.Totals[col.Key]
				if !exists {
					if !labelWritten {
						if row.Kind == "footer" {
							value = "Итого по отчёту"
						} else {
							value = "Итого по группе"
						}
						labelWritten = true
					}
					break
				}
				if amounts, ok := total.([]tabular.MoneyTotal); ok {
					// Distinct currency totals never become a combined number.
					lines := make([]string, len(amounts))
					for k, amount := range amounts {
						lines[k] = tabular.FormatTotal(amount)
					}
					value = strings.Join(lines, "\n")
					format = "@"
				} else {
					value = total
					format = "#,##0.####"
				}
			}
			id, err := style(row.Kind != "data", format)
			if err != nil {
				return err
			}
			cells[j] = excelize.Cell{Value: value, StyleID: id}
		}
		if err = sw.SetRow("A"+strconv.Itoa(i+4), cells, excelize.RowOpts{OutlineLevel: min(row.Depth, 7)}); err != nil {
			return err
		}
	}
	if err = sw.Flush(); err != nil {
		return err
	}
	return f.Write(w)
}

func moneyFormat(scale int) string {
	if scale == 0 {
		return "#,##0"
	}
	return "#,##0." + strings.Repeat("0", scale)
}

// CSV uses the same exact units as XLSX; strings are never passed via float64.
func CSV(w io.Writer, meta reportcontract.ReportMeta, items []map[string]any, columnKeys []string) error {
	columns := resolveExportColumns(meta, columnKeys)
	csvw := csv.NewWriter(w)
	header := make([]string, len(columns))
	for i, col := range columns {
		header[i] = col.Label
	}
	if err := csvw.Write(header); err != nil {
		return err
	}
	for _, item := range items {
		row := make([]string, len(columns))
		for i, col := range columns {
			value, err := tabular.FormatCell(item, col)
			if err != nil {
				return err
			}
			// Quoting alone doesn't stop spreadsheet formula interpretation.
			if col.Type != "money" && col.Type != "quantity" && len(value) > 0 && strings.ContainsRune("=+-@", rune(value[0])) {
				value = "'" + value
			}
			row[i] = value
		}
		if err := csvw.Write(row); err != nil {
			return err
		}
	}
	csvw.Flush()
	return csvw.Error()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveExportColumns returns columns in the order specified by exportColumnKeys.
// If exportColumnKeys is empty, falls back to all visible (non-hidden) columns.
// Unknown keys are silently skipped.
func resolveExportColumns(meta reportcontract.ReportMeta, exportColumnKeys []string) []reportcontract.ReportColumn {
	if len(exportColumnKeys) == 0 {
		return visibleColumns(meta)
	}

	// Build lookup map: key → column
	colMap := make(map[string]reportcontract.ReportColumn, len(meta.Columns))
	for _, c := range meta.Columns {
		colMap[c.Key] = c
	}

	// Resolve in user's order
	cols := make([]reportcontract.ReportColumn, 0, len(exportColumnKeys))
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

func visibleColumns(meta reportcontract.ReportMeta) []reportcontract.ReportColumn {
	cols := make([]reportcontract.ReportColumn, 0, len(meta.Columns))
	for _, c := range meta.Columns {
		if !c.DefaultHidden {
			cols = append(cols, c)
		}
	}
	return cols
}

func thinBorders() []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: "CCCCCC", Style: 1},
		{Type: "top", Color: "CCCCCC", Style: 1},
		{Type: "right", Color: "CCCCCC", Style: 1},
		{Type: "bottom", Color: "CCCCCC", Style: 1},
	}
}
