// Package compiler — server-side grouping engine.
// Port of frontend report-grouping.ts to Go.
// Used by the /grouped endpoint when totalItems > 5000 (browser threshold).
package compiler

import (
	"fmt"
	"sort"

	"metapus/internal/platform"
)

// DisplayRow mirrors the frontend discriminated union type.
// Kind: "group" | "data" | "subtotal" | "footer"
type DisplayRow struct {
	Kind   string             `json:"kind"`
	Depth  int                `json:"depth,omitempty"`
	Label  string             `json:"label,omitempty"`
	Count  int                `json:"count,omitempty"`
	Item   map[string]any     `json:"item,omitempty"`
	Totals map[string]float64 `json:"totals,omitempty"`
}

// BuildDisplayRows transforms flat items into grouped DisplayRow slice
// with group headers, subtotals, and a grand total footer.
// Mirrors the frontend buildDisplayRows() exactly.
func BuildDisplayRows(
	items []map[string]any,
	groupByKeys []string,
	totalDefs []platform.ReportTotal,
) []DisplayRow {
	if len(items) == 0 {
		return nil
	}

	// No grouping — flat data rows + footer
	if len(groupByKeys) == 0 {
		rows := make([]DisplayRow, 0, len(items)+1)
		for _, item := range items {
			rows = append(rows, DisplayRow{Kind: "data", Depth: 0, Item: item})
		}
		rows = append(rows, DisplayRow{Kind: "footer", Totals: computeTotals(items, totalDefs)})
		return rows
	}

	// Recursive grouping
	rows := buildGroupLevel(items, groupByKeys, 0, totalDefs)

	// Grand total footer
	rows = append(rows, DisplayRow{Kind: "footer", Totals: computeTotals(items, totalDefs)})

	return rows
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
	totalDefs []platform.ReportTotal,
) []DisplayRow {
	if depth >= len(groupByKeys) {
		rows := make([]DisplayRow, 0, len(items))
		for _, item := range items {
			rows = append(rows, DisplayRow{Kind: "data", Depth: depth, Item: item})
		}
		return rows
	}

	key := groupByKeys[depth]
	groups := groupByKey(items, key)
	var rows []DisplayRow

	for _, g := range groups {
		subtotals := computeTotals(g.items, totalDefs)

		// Group header
		rows = append(rows, DisplayRow{
			Kind:   "group",
			Depth:  depth,
			Label:  g.label,
			Count:  len(g.items),
			Totals: subtotals,
		})

		// Recurse
		rows = append(rows, buildGroupLevel(g.items, groupByKeys, depth+1, totalDefs)...)

		// Subtotal
		if len(totalDefs) > 0 {
			rows = append(rows, DisplayRow{
				Kind:   "subtotal",
				Depth:  depth,
				Totals: subtotals,
			})
		}
	}

	return rows
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

func computeTotals(items []map[string]any, totalDefs []platform.ReportTotal) map[string]float64 {
	result := make(map[string]float64, len(totalDefs))

	for _, def := range totalDefs {
		var sum float64
		var count int
		var min, max float64
		first := true

		for _, item := range items {
			v, ok := ToFloat64(item[def.Column])
			if !ok {
				continue
			}
			count++
			sum += v
			if first || v < min {
				min = v
			}
			if first || v > max {
				max = v
			}
			first = false
		}

		switch def.Func {
		case "sum":
			result[def.Column] = sum
		case "count":
			result[def.Column] = float64(count)
		case "avg":
			if count > 0 {
				result[def.Column] = sum / float64(count)
			}
		case "min":
			result[def.Column] = min
		case "max":
			result[def.Column] = max
		}
	}

	return result
}

func compareValues(a, b any) int {
	// Numeric comparison
	if na, ok := ToFloat64(a); ok {
		if nb, ok := ToFloat64(b); ok {
			if na < nb {
				return -1
			}
			if na > nb {
				return 1
			}
			return 0
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
