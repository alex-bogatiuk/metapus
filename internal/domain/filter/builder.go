package filter

import (
	"fmt"
	"strings"
)

// ExtractSearchQuery removes the __search pseudo-item from a filter slice,
// returning the search query and the remaining "real" items.
func ExtractSearchQuery(items []Item) (string, []Item) {
	var query string
	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		if item.Field == SearchFieldName {
			if s, ok := item.Value.(string); ok {
				query = s
			}
			continue
		}
		filtered = append(filtered, item)
	}
	return strings.TrimSpace(query), filtered
}

// BuildValidCols constructs a whitelist of valid column names from selectCols
// plus any extra columns. Used for column validation in filters.
func BuildValidCols(selectCols []string, extra ...string) map[string]struct{} {
	cols := make(map[string]struct{}, len(selectCols)+len(extra))
	for _, col := range selectCols {
		cols[col] = struct{}{}
	}
	for _, col := range extra {
		cols[col] = struct{}{}
	}
	return cols
}

// BuildOrderCols constructs a whitelist of valid column names for ORDER BY.
func BuildOrderCols(selectCols []string, extra ...string) map[string]struct{} {
	return BuildValidCols(selectCols, extra...)
}

// ValidateItems checks that all filter items have valid structure
// before they are processed. Fails fast on first error.
func ValidateItems(items []Item) error {
	for i, item := range items {
		if item.Field == "" {
			return fmt.Errorf("filter[%d]: field is empty", i)
		}
		if !isValidOperator(item.Operator) {
			return fmt.Errorf("filter[%d]: unknown operator %q", i, item.Operator)
		}
		// Operators that don't require a value
		if item.Operator == IsNull || item.Operator == IsNotNull {
			continue
		}
		if item.Value == nil {
			return fmt.Errorf("filter[%d]: value is required for operator %q", i, item.Operator)
		}
	}
	return nil
}

// isValidOperator checks if the given operator is one of the known ComparisonTypes.
func isValidOperator(op ComparisonType) bool {
	switch op {
	case Equal, NotEqual, LessOrEqual, GreaterOrEqual, Less, Greater,
		InList, NotInList, Contains, NotContains,
		InHierarchy, NotInHierarchy, IsNull, IsNotNull:
		return true
	default:
		return false
	}
}
