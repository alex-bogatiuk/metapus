// Server-side grouping uses the same unit-aware aggregates as export.
// Port of frontend report-grouping.ts to Go.
// Used by the /grouped endpoint when totalItems > 5000 (browser threshold).
package tabular

import (
	"fmt"
	"sort"

	reportcontract "metapus/internal/domain/reports/contract"
)

// DisplayRow mirrors the frontend discriminated union type.
// Kind: "group" | "data" | "subtotal" | "footer"
type DisplayRow struct {
	Kind   string         `json:"kind"`
	Depth  int            `json:"depth,omitzero"`
	Label  string         `json:"label,omitempty"`
	Count  int            `json:"count,omitzero"`
	Item   map[string]any `json:"item,omitempty"`
	Totals map[string]any `json:"totals,omitempty"`
}

// BuildDisplayRows transforms flat items into grouped DisplayRow slice
// with group headers, subtotals, and a grand total footer.
// Mirrors the frontend buildDisplayRows() exactly.
func BuildDisplayRows(
	items []map[string]any,
	groupByKeys []string,
	totalDefs []reportcontract.ReportTotal,
) ([]DisplayRow, error) {
	if len(items) == 0 {
		return nil, nil
	}

	totals, err := Aggregate(items, totalDefs)
	if err != nil {
		return nil, err
	}
	// No grouping — flat data rows + footer
	if len(groupByKeys) == 0 {
		rows := make([]DisplayRow, 0, len(items)+1)
		for _, item := range items {
			rows = append(rows, DisplayRow{Kind: "data", Depth: 0, Item: item})
		}
		rows = append(rows, DisplayRow{Kind: "footer", Totals: totals})
		return rows, nil
	}

	// Recursive grouping
	rows, err := buildGroupLevel(items, groupByKeys, 0, totalDefs)
	if err != nil {
		return nil, err
	}

	// Grand total footer
	rows = append(rows, DisplayRow{Kind: "footer", Totals: totals})

	return rows, nil
}

// SortItems sorts items by column key and direction.
func SortItems(items []map[string]any, column string, direction string) []map[string]any {
	sorted := make([]map[string]any, len(items))
	copy(sorted, items)

	sort.SliceStable(sorted, func(i, j int) bool {
		va := sorted[i][column]
		vb := sorted[j][column]
		cmp := compareValues(va, vb)
		if direction == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})

	return sorted
}

// --- Internal helpers ---

func buildGroupLevel(
	items []map[string]any,
	groupByKeys []string,
	depth int,
	totalDefs []reportcontract.ReportTotal,
) ([]DisplayRow, error) {
	if depth >= len(groupByKeys) {
		rows := make([]DisplayRow, 0, len(items))
		for _, item := range items {
			rows = append(rows, DisplayRow{Kind: "data", Depth: depth, Item: item})
		}
		return rows, nil
	}

	key := groupByKeys[depth]
	groups := groupByKey(items, key)
	var rows []DisplayRow

	for _, g := range groups {
		subtotals, err := Aggregate(g.items, totalDefs)
		if err != nil {
			return nil, err
		}

		// Group header
		rows = append(rows, DisplayRow{
			Kind:   "group",
			Depth:  depth,
			Label:  g.label,
			Count:  len(g.items),
			Totals: subtotals,
		})

		// Recurse
		children, err := buildGroupLevel(g.items, groupByKeys, depth+1, totalDefs)
		if err != nil {
			return nil, err
		}
		rows = append(rows, children...)

		// Subtotal
		if len(totalDefs) > 0 {
			rows = append(rows, DisplayRow{
				Kind:   "subtotal",
				Depth:  depth,
				Totals: subtotals,
			})
		}
	}

	return rows, nil
}

type group struct {
	label string
	items []map[string]any
}

// groupByKey groups items by a key, preserving insertion order.
func groupByKey(items []map[string]any, key string) []group {
	orderMap := make(map[string]int)
	var groups []group

	for _, item := range items {
		val := ToString(item[key])
		idx, exists := orderMap[val]
		if !exists {
			idx = len(groups)
			orderMap[val] = idx
			groups = append(groups, group{label: val})
		}
		groups[idx].items = append(groups[idx].items, item)
	}

	return groups
}

func compareValues(a, b any) int {
	// Exact comparison also covers string minor units above 2^53.
	if na, err := Numeric(a); err == nil {
		if nb, err := Numeric(b); err == nil {
			return na.Cmp(nb)
		}
	}
	// String fallback
	sa := ToString(a)
	sb := ToString(b)
	if sa < sb {
		return -1
	}
	if sa > sb {
		return 1
	}
	return 0
}

// ToString converts any value to string for display.
func ToString(v any) string {
	if v == nil {
		return "—"
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// ToFloat64 converts numeric interface values to float64.
func ToFloat64(v any) (float64, bool) {
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
