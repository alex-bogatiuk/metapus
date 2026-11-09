package compiler

import (
	reportcontract "metapus/internal/domain/reports/contract"
	"metapus/internal/domain/reports/tabular"
)

type DisplayRow = tabular.DisplayRow

func BuildDisplayRows(items []map[string]any, groupBy []string, totals []reportcontract.ReportTotal) ([]DisplayRow, error) {
	return tabular.BuildDisplayRows(items, groupBy, totals)
}
func SortItems(items []map[string]any, column, direction string) []map[string]any {
	return tabular.SortItems(items, column, direction)
}
