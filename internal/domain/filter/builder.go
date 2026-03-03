package filter

import (
	"fmt"

	"github.com/Masterminds/squirrel"
)

// BuildValidCols constructs a whitelist of valid column names from selectCols
// plus any extra columns. Used for SQL injection protection in filters.
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
// before they are translated to SQL. Fails fast on first error.
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

// BuildConditions translates []Item into []squirrel.Sqlizer.
//
// validCols is a whitelist of allowed column names (SQL injection protection).
// tableName is needed for InHierarchy/NotInHierarchy operators (recursive CTE).
//
// This is the shared core of the filtering engine, used by both
// BaseCatalogRepo and BaseDocumentRepo.
func BuildConditions(items []Item, validCols map[string]struct{}, tableName string) ([]squirrel.Sqlizer, error) {
	if err := ValidateItems(items); err != nil {
		return nil, err
	}

	var conditions []squirrel.Sqlizer

	for _, item := range items {
		if _, ok := validCols[item.Field]; !ok {
			return nil, fmt.Errorf("invalid filter column: %s", item.Field)
		}

		fieldExpr := item.Field
		if item.FieldType == "date" {
			fieldExpr = "DATE(" + item.Field + ")"
		}

		switch item.Operator {
		case Equal:
			conditions = append(conditions, squirrel.Eq{fieldExpr: item.Value})
		case NotEqual:
			// (field <> value OR field IS NULL) — include rows where field is NULL
			conditions = append(conditions, squirrel.Or{
				squirrel.NotEq{fieldExpr: item.Value},
				squirrel.Eq{fieldExpr: nil},
			})
		case LessOrEqual:
			conditions = append(conditions, squirrel.LtOrEq{fieldExpr: item.Value})
		case GreaterOrEqual:
			conditions = append(conditions, squirrel.GtOrEq{fieldExpr: item.Value})
		case Less:
			conditions = append(conditions, squirrel.Lt{fieldExpr: item.Value})
		case Greater:
			conditions = append(conditions, squirrel.Gt{fieldExpr: item.Value})
		case InList:
			conditions = append(conditions, squirrel.Eq{fieldExpr: item.Value})
		case NotInList:
			// (field NOT IN (...) OR field IS NULL) — include rows where field is NULL
			conditions = append(conditions, squirrel.Or{
				squirrel.NotEq{fieldExpr: item.Value},
				squirrel.Eq{fieldExpr: nil},
			})
		case IsNull:
			conditions = append(conditions, squirrel.Eq{fieldExpr: nil})
		case IsNotNull:
			conditions = append(conditions, squirrel.NotEq{fieldExpr: nil})
		case Contains:
			val := fmt.Sprintf("%%%v%%", item.Value)
			conditions = append(conditions, squirrel.ILike{fieldExpr: val})
		case NotContains:
			// (field NOT ILIKE '%val%' OR field IS NULL) — include rows where field is NULL
			val := fmt.Sprintf("%%%v%%", item.Value)
			conditions = append(conditions, squirrel.Or{
				squirrel.NotILike{fieldExpr: val},
				squirrel.Eq{fieldExpr: nil},
			})
		case InHierarchy:
			cteSQL := fmt.Sprintf(`
                id IN (
                    WITH RECURSIVE hierarchy AS (
                        SELECT id FROM %s WHERE id = $1 
                        UNION ALL 
                        SELECT t.id FROM %s t JOIN hierarchy h ON t.parent_id = h.id
                    ) 
                    SELECT id FROM hierarchy
                )
            `, tableName, tableName)
			conditions = append(conditions, squirrel.Expr(cteSQL, item.Value))
		case NotInHierarchy:
			cteSQL := fmt.Sprintf(`
                (id NOT IN (
                    WITH RECURSIVE hierarchy AS (
                        SELECT id FROM %s WHERE id = $1 
                        UNION ALL 
                        SELECT t.id FROM %s t JOIN hierarchy h ON t.parent_id = h.id
                    ) 
                    SELECT id FROM hierarchy
                ) OR id IS NULL)
            `, tableName, tableName)
			conditions = append(conditions, squirrel.Expr(cteSQL, item.Value))
		}
	}

	return conditions, nil
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
