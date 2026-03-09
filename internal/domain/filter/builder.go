package filter

import (
	"fmt"
	"math"
	"strconv"
	"strings"

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

		// Money fields use dynamic SQL-side scaling via currency_id → cat_currencies.minor_multiplier.
		// The scale depends on the document's currency, not a static constant.
		if item.FieldType == "money" {
			cond, err := buildMoneyCondition(item.Field, item.Operator, item.Value, tableName)
			if err != nil {
				return nil, err
			}
			conditions = append(conditions, cond)
			continue
		}

		// Apply static storage scale to the user-visible value (e.g. Quantity ×10000).
		val := scaleFilterValue(item.Value, item.Scale)

		fieldExpr := item.Field
		if item.FieldType == "date" {
			fieldExpr = "DATE(" + item.Field + ")"
		}

		switch item.Operator {
		case Equal:
			conditions = append(conditions, squirrel.Eq{fieldExpr: val})
		case NotEqual:
			// (field <> value OR field IS NULL) — include rows where field is NULL
			conditions = append(conditions, squirrel.Or{
				squirrel.NotEq{fieldExpr: val},
				squirrel.Eq{fieldExpr: nil},
			})
		case LessOrEqual:
			conditions = append(conditions, squirrel.LtOrEq{fieldExpr: val})
		case GreaterOrEqual:
			conditions = append(conditions, squirrel.GtOrEq{fieldExpr: val})
		case Less:
			conditions = append(conditions, squirrel.Lt{fieldExpr: val})
		case Greater:
			conditions = append(conditions, squirrel.Gt{fieldExpr: val})
		case InList:
			conditions = append(conditions, squirrel.Eq{fieldExpr: val})
		case NotInList:
			// (field NOT IN (...) OR field IS NULL) — include rows where field is NULL
			conditions = append(conditions, squirrel.Or{
				squirrel.NotEq{fieldExpr: val},
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

// BuildTablePartCondition generates an EXISTS (or NOT EXISTS) subquery
// for filtering parent rows by a child table part column.
//
// Semantics:
//   - Positive operators (eq, in, contains, gt, gte, lt, lte):
//     EXISTS (SELECT 1 FROM child WHERE child.fk = parent.id AND child.col <op> value)
//   - Negative operators (neq, nin, ncontains):
//     NOT EXISTS (SELECT 1 FROM child WHERE child.fk = parent.id AND child.col = value)
//   - null:  EXISTS  ... AND col IS NULL
//   - not_null: NOT EXISTS ... AND col IS NULL
//
// parentTable is the main table (e.g. "doc_goods_receipts").
// tp describes the child table.
// column is the validated column name within the child table.
func BuildTablePartCondition(item Item, parentTable string, tp TablePartInfo, column string) (squirrel.Sqlizer, error) {
	if _, ok := tp.ValidCols[column]; !ok {
		return nil, fmt.Errorf("invalid table part filter column: %s", item.Field)
	}

	if err := ValidateItems([]Item{item}); err != nil {
		return nil, err
	}

	// Apply static storage scale to the user-visible value (e.g. Quantity ×10000).
	val := scaleFilterValue(item.Value, item.Scale)

	// For money fields, use dynamic SQL-side scaling via parent table's currency.
	// valuePH is the SQL placeholder for the filter value: "?" for normal fields,
	// or a subquery expression for money fields.
	valuePH := "?"
	if item.FieldType == "money" {
		valuePH = fmt.Sprintf(
			"ROUND(CAST(? AS NUMERIC) * (SELECT minor_multiplier FROM cat_currencies WHERE id = %s.currency_id))",
			parentTable,
		)
	}

	fieldExpr := column
	if item.FieldType == "date" {
		fieldExpr = "DATE(" + column + ")"
	}

	// Determine the inner condition and whether to negate EXISTS.
	var innerCond string
	var args []interface{}
	negate := false

	switch item.Operator {
	case Equal:
		innerCond = fieldExpr + " = " + valuePH
		args = []interface{}{val}
	case NotEqual:
		// "document has NO lines with col = value"
		innerCond = fieldExpr + " = " + valuePH
		args = []interface{}{val}
		negate = true
	case LessOrEqual:
		innerCond = fieldExpr + " <= " + valuePH
		args = []interface{}{val}
	case GreaterOrEqual:
		innerCond = fieldExpr + " >= " + valuePH
		args = []interface{}{val}
	case Less:
		innerCond = fieldExpr + " < " + valuePH
		args = []interface{}{val}
	case Greater:
		innerCond = fieldExpr + " > " + valuePH
		args = []interface{}{val}
	case InList:
		if item.FieldType == "money" {
			return nil, fmt.Errorf("InList operator not supported for money field with dynamic currency scaling")
		}
		// Build IN (?, ?, ...) from slice
		placeholders, inArgs := expandSlice(val)
		if len(inArgs) == 0 {
			// Empty list → no match
			return squirrel.Expr("FALSE"), nil
		}
		innerCond = fieldExpr + " IN (" + placeholders + ")"
		args = inArgs
	case NotInList:
		if item.FieldType == "money" {
			return nil, fmt.Errorf("NotInList operator not supported for money field with dynamic currency scaling")
		}
		placeholders, inArgs := expandSlice(val)
		if len(inArgs) == 0 {
			return squirrel.Expr("TRUE"), nil
		}
		innerCond = fieldExpr + " IN (" + placeholders + ")"
		args = inArgs
		negate = true
	case Contains:
		innerCond = fieldExpr + " ILIKE ?"
		args = []interface{}{fmt.Sprintf("%%%v%%", val)}
	case NotContains:
		innerCond = fieldExpr + " ILIKE ?"
		args = []interface{}{fmt.Sprintf("%%%v%%", val)}
		negate = true
	case IsNull:
		innerCond = fieldExpr + " IS NULL"
	case IsNotNull:
		innerCond = fieldExpr + " IS NULL"
		negate = true
	default:
		return nil, fmt.Errorf("unsupported operator for table part filter: %s", item.Operator)
	}

	existsKeyword := "EXISTS"
	if negate {
		existsKeyword = "NOT EXISTS"
	}

	sql := fmt.Sprintf(
		"%s (SELECT 1 FROM %s WHERE %s.%s = %s.id AND %s)",
		existsKeyword, tp.TableName, tp.TableName, tp.ForeignKey, parentTable, innerCond,
	)

	return squirrel.Expr(sql, args...), nil
}

// BuildReferenceFieldCondition generates an EXISTS (or NOT EXISTS) subquery
// for filtering parent rows by a referenced entity column (deep filtering).
//
// Semantics:
//   - Positive operators (eq, in, contains, gt, gte, lt, lte):
//     EXISTS (SELECT 1 FROM ref_table WHERE ref_table.id = parent.fk AND ref_table.col <op> value)
//   - Negative operators (neq, nin, ncontains):
//     NOT EXISTS (SELECT 1 FROM ref_table WHERE ref_table.id = parent.fk AND ref_table.col = value)
//   - null:     EXISTS ... AND col IS NULL
//   - not_null: NOT EXISTS ... AND col IS NULL
func BuildReferenceFieldCondition(item Item, parentTable string, refInfo ReferenceFieldInfo, column string) (squirrel.Sqlizer, error) {
	if _, ok := refInfo.ValidCols[column]; !ok {
		return nil, fmt.Errorf("invalid reference filter column: %s", item.Field)
	}

	if err := ValidateItems([]Item{item}); err != nil {
		return nil, err
	}

	val := scaleFilterValue(item.Value, item.Scale)
	valuePH := "?"

	fieldExpr := column
	if item.FieldType == "date" {
		fieldExpr = "DATE(" + column + ")"
	}

	var innerCond string
	var args []interface{}
	negate := false

	switch item.Operator {
	case Equal:
		innerCond = fieldExpr + " = " + valuePH
		args = []interface{}{val}
	case NotEqual:
		innerCond = fieldExpr + " = " + valuePH
		args = []interface{}{val}
		negate = true
	case LessOrEqual:
		innerCond = fieldExpr + " <= " + valuePH
		args = []interface{}{val}
	case GreaterOrEqual:
		innerCond = fieldExpr + " >= " + valuePH
		args = []interface{}{val}
	case Less:
		innerCond = fieldExpr + " < " + valuePH
		args = []interface{}{val}
	case Greater:
		innerCond = fieldExpr + " > " + valuePH
		args = []interface{}{val}
	case InList:
		placeholders, inArgs := expandSlice(val)
		if len(inArgs) == 0 {
			return squirrel.Expr("FALSE"), nil
		}
		innerCond = fieldExpr + " IN (" + placeholders + ")"
		args = inArgs
	case NotInList:
		placeholders, inArgs := expandSlice(val)
		if len(inArgs) == 0 {
			return squirrel.Expr("TRUE"), nil
		}
		innerCond = fieldExpr + " IN (" + placeholders + ")"
		args = inArgs
		negate = true
	case Contains:
		innerCond = fieldExpr + " ILIKE ?"
		args = []interface{}{fmt.Sprintf("%%%v%%", val)}
	case NotContains:
		innerCond = fieldExpr + " ILIKE ?"
		args = []interface{}{fmt.Sprintf("%%%v%%", val)}
		negate = true
	case IsNull:
		innerCond = fieldExpr + " IS NULL"
	case IsNotNull:
		innerCond = fieldExpr + " IS NULL"
		negate = true
	default:
		return nil, fmt.Errorf("unsupported operator for reference filter: %s", item.Operator)
	}

	existsKeyword := "EXISTS"
	if negate {
		existsKeyword = "NOT EXISTS"
	}

	sql := fmt.Sprintf(
		"%s (SELECT 1 FROM %s WHERE %s.id = %s.%s AND %s)",
		existsKeyword, refInfo.TableName, refInfo.TableName, parentTable, refInfo.ForeignKey, innerCond,
	)

	return squirrel.Expr(sql, args...), nil
}

// expandSlice converts an interface{} value (expected to be a slice) into
// a list of "?" placeholders and a flat []interface{} of args.
// Used by BuildTablePartCondition for IN (...) clauses.
func expandSlice(value interface{}) (string, []interface{}) {
	slice, ok := value.([]interface{})
	if !ok {
		// Try typed string slice (common from JSON unmarshal)
		if ss, ok2 := value.([]string); ok2 {
			args := make([]interface{}, len(ss))
			phs := make([]string, len(ss))
			for i, s := range ss {
				args[i] = s
				phs[i] = "?"
			}
			return strings.Join(phs, ", "), args
		}
		// Single value fallback
		if value == nil {
			return "", nil
		}
		return "?", []interface{}{value}
	}
	if len(slice) == 0 {
		return "", nil
	}
	args := make([]interface{}, len(slice))
	phs := make([]string, len(slice))
	for i, v := range slice {
		args[i] = v
		phs[i] = "?"
	}
	return strings.Join(phs, ", "), args
}

// buildMoneyCondition generates a SQL condition for money-type fields with dynamic
// scaling via currency_id → cat_currencies.minor_multiplier.
// tableName is the table containing the currency_id column.
// Uses ROUND(CAST(? AS NUMERIC) * minor_multiplier) to avoid floating-point issues.
func buildMoneyCondition(fieldExpr string, op ComparisonType, value any, tableName string) (squirrel.Sqlizer, error) {
	mul := fmt.Sprintf(
		"ROUND(CAST(? AS NUMERIC) * (SELECT minor_multiplier FROM cat_currencies WHERE id = %s.currency_id))",
		tableName,
	)

	switch op {
	case Equal:
		return squirrel.Expr(fmt.Sprintf("%s = %s", fieldExpr, mul), value), nil
	case NotEqual:
		sql := fmt.Sprintf("(%s <> %s OR %s IS NULL)", fieldExpr, mul, fieldExpr)
		return squirrel.Expr(sql, value), nil
	case LessOrEqual:
		return squirrel.Expr(fmt.Sprintf("%s <= %s", fieldExpr, mul), value), nil
	case GreaterOrEqual:
		return squirrel.Expr(fmt.Sprintf("%s >= %s", fieldExpr, mul), value), nil
	case Less:
		return squirrel.Expr(fmt.Sprintf("%s < %s", fieldExpr, mul), value), nil
	case Greater:
		return squirrel.Expr(fmt.Sprintf("%s > %s", fieldExpr, mul), value), nil
	case IsNull:
		return squirrel.Eq{fieldExpr: nil}, nil
	case IsNotNull:
		return squirrel.NotEq{fieldExpr: nil}, nil
	default:
		return nil, fmt.Errorf("operator %s not supported for money field with dynamic currency scaling", op)
	}
}

// scaleFilterValue multiplies a numeric filter value by the given scale.
// Used to convert user-visible values (e.g. 10 for Quantity) to internal
// storage values (e.g. 100000 = 10 × 10000).
// Handles float64/int/string scalars and []interface{} slices.
// Returns the original value unchanged if scale <= 1.
func scaleFilterValue(value any, scale int) any {
	if scale <= 1 || value == nil {
		return value
	}
	s := float64(scale)

	switch v := value.(type) {
	case float64:
		return int64(math.Round(v * s))
	case int:
		return int64(v) * int64(scale)
	case int64:
		return v * int64(scale)
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return value
		}
		return int64(math.Round(f * s))
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, elem := range v {
			result[i] = scaleFilterValue(elem, scale)
		}
		return result
	default:
		return value
	}
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
