// Package filter provides PostgreSQL query generation for domain filter criteria using squirrel.
package filter

import (
	"fmt"
	"strings"

	"github.com/Masterminds/squirrel"

	"metapus/internal/domain/filter"
)

// BuildSearchConditions generates AND-combined ILIKE conditions for fuzzy search (1С-style).
//
// Example: query="красн авто", searchCols=["name"] →
//
//	name ILIKE '%красн%' AND name ILIKE '%авто%'
//
// Each token (space-separated word) must match at least one of the searchCols (OR).
// Between tokens — AND (all tokens must be present).
// Works with pg_trgm GIN indexes for fast ILIKE on large tables.
func BuildSearchConditions(query string, searchCols []string) squirrel.Sqlizer {
	tokens := strings.Fields(query)
	if len(tokens) == 0 || len(searchCols) == 0 {
		return nil
	}

	ands := make(squirrel.And, 0, len(tokens))
	for _, token := range tokens {
		escaped := escapeLikePattern(token)
		pattern := "%" + escaped + "%"
		ors := make(squirrel.Or, 0, len(searchCols))
		for _, col := range searchCols {
			ors = append(ors, squirrel.ILike{col: pattern})
		}
		ands = append(ands, ors)
	}
	return ands
}

// escapeLikePattern escapes SQL LIKE special characters (%, _, \)
// so that user input is treated as literal text, not wildcards.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// BuildSingleCondition translates a single resolved filter Item into a squirrel.Sqlizer condition.
//
// sqlExpr is the fully resolved SQL expression for the field, e.g.:
//   - "base.merchant_id"            (direct column)
//   - "j1.name"                     (joined reference attribute)
//   - "DATE(base.date)"             (date-wrapped)
func BuildSingleCondition(item filter.ResolvedItem, sqlExpr string) (squirrel.Sqlizer, error) {
	val := item.Value()
	if item.Kind() == filter.DateValue {
		sqlExpr = "DATE(" + sqlExpr + ")"
	}

	switch item.Operator() {
	case filter.Equal:
		return squirrel.Eq{sqlExpr: val}, nil
	case filter.NotEqual:
		return squirrel.Or{
			squirrel.NotEq{sqlExpr: val},
			squirrel.Eq{sqlExpr: nil},
		}, nil
	case filter.LessOrEqual:
		return squirrel.LtOrEq{sqlExpr: val}, nil
	case filter.GreaterOrEqual:
		return squirrel.GtOrEq{sqlExpr: val}, nil
	case filter.Less:
		return squirrel.Lt{sqlExpr: val}, nil
	case filter.Greater:
		return squirrel.Gt{sqlExpr: val}, nil
	case filter.InList:
		return squirrel.Eq{sqlExpr: val}, nil
	case filter.NotInList:
		return squirrel.Or{
			squirrel.NotEq{sqlExpr: val},
			squirrel.Eq{sqlExpr: nil},
		}, nil
	case filter.IsNull:
		return squirrel.Eq{sqlExpr: nil}, nil
	case filter.IsNotNull:
		return squirrel.NotEq{sqlExpr: nil}, nil
	case filter.Contains:
		return squirrel.ILike{sqlExpr: fmt.Sprintf("%%%v%%", item.Value())}, nil
	case filter.NotContains:
		return squirrel.Or{
			squirrel.NotILike{sqlExpr: fmt.Sprintf("%%%v%%", item.Value())},
			squirrel.Eq{sqlExpr: nil},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported operator for BuildSingleCondition: %s", item.Operator())
	}
}

// BuildConditions translates []filter.Item into []squirrel.Sqlizer.
//
// fields is the server-declared column whitelist and storage semantics.
// tableName is needed for InHierarchy/NotInHierarchy operators (recursive CTE).
//
// This is the shared core of the filtering engine, used by both
// BaseCatalogRepo and BaseDocumentRepo.
func BuildConditions(items []filter.Item, fields map[string]filter.FieldSpec, tableName string) ([]squirrel.Sqlizer, error) {
	conditions := make([]squirrel.Sqlizer, 0, len(items))
	for _, item := range items {
		spec, ok := fields[item.Field]
		if !ok {
			return nil, fmt.Errorf("invalid filter column: %s", item.Field)
		}
		resolved, err := filter.Resolve(item, spec)
		if err != nil {
			return nil, fmt.Errorf("filter %s: %w", item.Field, err)
		}
		if resolved.Operator() == filter.InHierarchy || resolved.Operator() == filter.NotInHierarchy {
			conditions = append(conditions, buildHierarchyCondition(resolved, tableName))
			continue
		}
		condition, err := BuildSingleCondition(resolved, item.Field)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	return conditions, nil
}

// buildHierarchyCondition generates a recursive CTE condition for InHierarchy/NotInHierarchy.
func buildHierarchyCondition(item filter.ResolvedItem, tableName string) squirrel.Sqlizer {
	if item.Operator() == filter.InHierarchy {
		cteSQL := fmt.Sprintf(`
                id IN (
                    WITH RECURSIVE hierarchy AS (
                        SELECT id FROM %s WHERE id = ? 
                        UNION
                        SELECT t.id FROM %s t JOIN hierarchy h ON t.parent_id = h.id
                    ) 
                    SELECT id FROM hierarchy
                )
            `, tableName, tableName)
		return squirrel.Expr(cteSQL, item.Value())
	}
	// NotInHierarchy
	cteSQL := fmt.Sprintf(`
                (id NOT IN (
                    WITH RECURSIVE hierarchy AS (
                        SELECT id FROM %s WHERE id = ? 
                        UNION
                        SELECT t.id FROM %s t JOIN hierarchy h ON t.parent_id = h.id
                    ) 
                    SELECT id FROM hierarchy
                ) OR id IS NULL)
            `, tableName, tableName)
	return squirrel.Expr(cteSQL, item.Value())
}

// BuildTablePartCondition filters a parent by an authorized child column.
func BuildTablePartCondition(item filter.Item, parentTable string, tp filter.TablePartInfo, column string) (squirrel.Sqlizer, error) {
	return buildRelatedCondition(item, tp.Fields, tp.TableName, column,
		tp.TableName+"."+tp.ForeignKey+" = "+parentTable+".id")
}

// BuildReferenceFieldCondition filters a parent by a registered reference column.
func BuildReferenceFieldCondition(item filter.Item, parentTable string, ref filter.ReferenceFieldInfo, column string) (squirrel.Sqlizer, error) {
	return buildRelatedCondition(item, ref.Fields, ref.TableName, column,
		ref.TableName+".id = "+parentTable+"."+ref.ForeignKey)
}

// Negative related filters mean that no matching child exists. This preserves
// the same NOT EXISTS semantics for table parts and nullable references.
func buildRelatedCondition(item filter.Item, fields map[string]filter.FieldSpec, table, column, link string) (squirrel.Sqlizer, error) {
	spec, ok := fields[column]
	if !ok {
		return nil, fmt.Errorf("invalid related filter column: %s", item.Field)
	}
	resolved, err := filter.Resolve(item, spec)
	if err != nil {
		return nil, fmt.Errorf("filter %s: %w", item.Field, err)
	}
	negate := true
	op := resolved.Operator()
	switch op {
	case filter.NotEqual:
		op = filter.Equal
	case filter.NotInList:
		op = filter.InList
	case filter.NotContains:
		op = filter.Contains
	case filter.IsNotNull:
		op = filter.IsNull
	default:
		negate = false
	}
	resolved = resolved.WithOperator(op)

	condition, err := BuildSingleCondition(resolved, table+"."+column)
	if err != nil {
		return nil, err
	}
	query := squirrel.Select("1").From(table).Where(squirrel.Expr(link)).Where(condition)
	if negate {
		return squirrel.Expr("NOT EXISTS (?)", query), nil
	}
	return squirrel.Expr("EXISTS (?)", query), nil
}
