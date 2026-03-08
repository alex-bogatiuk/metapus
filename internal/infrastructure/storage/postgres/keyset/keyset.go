// Package keyset provides cursor-based (keyset) pagination helpers
// for building efficient SQL queries using tuple comparison.
//
// Instead of OFFSET/LIMIT, this uses WHERE (sort_col, id) > ($val, $id)
// with a composite B-Tree index for O(log N) performance at any depth.
package keyset

import (
	"fmt"
	"strings"

	"github.com/Masterminds/squirrel"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/cursor"
)

// SortSpec describes the parsed ORDER BY specification.
type SortSpec struct {
	Field     string // e.g. "date", "name"
	Direction string // "ASC" or "DESC"
}

// ParseOrderBy parses an orderBy string like "-date" or "name" into a SortSpec.
// Returns the field and direction. Validates the field against allowed columns.
func ParseOrderBy(orderBy string, defaultField string, defaultDir string, allowedCols map[string]struct{}) (SortSpec, error) {
	if strings.TrimSpace(orderBy) == "" {
		return SortSpec{Field: defaultField, Direction: defaultDir}, nil
	}

	direction := "ASC"
	field := orderBy
	if strings.HasPrefix(orderBy, "-") {
		direction = "DESC"
		field = strings.TrimPrefix(orderBy, "-")
	} else if strings.HasPrefix(orderBy, "+") {
		field = strings.TrimPrefix(orderBy, "+")
	}

	field = strings.TrimSpace(field)
	if field == "" {
		return SortSpec{}, apperror.NewValidation("invalid orderBy").WithDetail("orderBy", orderBy)
	}

	if _, ok := allowedCols[field]; !ok {
		return SortSpec{}, apperror.NewValidation("invalid orderBy").WithDetail("orderBy", orderBy).WithDetail("field", field)
	}

	return SortSpec{Field: field, Direction: direction}, nil
}

// OrderByClause returns the SQL ORDER BY clause for this spec with id as tie-breaker.
// e.g. "date DESC, id DESC"
func (s SortSpec) OrderByClause() string {
	return s.Field + " " + s.Direction + ", id " + s.Direction
}

// InvertedOrderByClause returns the inverted ORDER BY (for backward fetching).
// e.g. "date ASC, id ASC" if original was "date DESC, id DESC"
func (s SortSpec) InvertedOrderByClause() string {
	inv := "ASC"
	if s.Direction == "ASC" {
		inv = "DESC"
	}
	return s.Field + " " + inv + ", id " + inv
}

// CursorFields returns the field names stored in cursors for this sort spec.
func (s SortSpec) CursorFields() []string {
	return []string{s.Field, "id"}
}

// TupleCondition builds a WHERE clause for cursor-based pagination using tuple comparison.
//
// For forward pagination (after cursor, original sort order):
//   - DESC sort: (field, id) < ($val, $id)
//   - ASC sort:  (field, id) > ($val, $id)
//
// For backward pagination (before cursor, inverted sort order):
//   - DESC sort: (field, id) > ($val, $id)   (inverted)
//   - ASC sort:  (field, id) < ($val, $id)   (inverted)
func TupleCondition(spec SortSpec, values []any, forward bool) squirrel.Sqlizer {
	op := ">"
	if spec.Direction == "DESC" {
		op = "<"
	}
	if !forward {
		// Invert for backward
		if op == ">" {
			op = "<"
		} else {
			op = ">"
		}
	}

	// PostgreSQL tuple comparison: (col1, col2) op ($1, $2)
	sql := fmt.Sprintf("(%s, id) %s (?, ?)", spec.Field, op)
	return squirrel.Expr(sql, values[0], values[1])
}

// TupleConditionInclusive is like TupleCondition but uses >= or <= (inclusive of boundary).
// Used for the "around" query to include the target row itself.
func TupleConditionInclusive(spec SortSpec, values []any, forward bool) squirrel.Sqlizer {
	op := ">="
	if spec.Direction == "DESC" {
		op = "<="
	}
	if !forward {
		if op == ">=" {
			op = "<="
		} else {
			op = ">="
		}
	}

	sql := fmt.Sprintf("(%s, id) %s (?, ?)", spec.Field, op)
	return squirrel.Expr(sql, values[0], values[1])
}

// BuildCursorFromRow creates an opaque cursor token from a row's sort field value and id.
func BuildCursorFromRow(spec SortSpec, sortValue any, idValue any) (string, error) {
	return cursor.Encode(spec.CursorFields(), []any{sortValue, idValue})
}

// DecodeCursor decodes an opaque cursor and validates that the sort fields match.
func DecodeCursor(token string, spec SortSpec) ([]any, error) {
	payload, err := cursor.Decode(token)
	if err != nil {
		return nil, apperror.NewValidation("invalid cursor").WithCause(err)
	}

	expectedFields := spec.CursorFields()
	if len(payload.Fields) != len(expectedFields) {
		return nil, apperror.NewValidation("cursor sort mismatch: expected fields " + strings.Join(expectedFields, ","))
	}
	for i, f := range expectedFields {
		if payload.Fields[i] != f {
			return nil, apperror.NewValidation("cursor sort mismatch").
				WithDetail("expected", strings.Join(expectedFields, ",")).
				WithDetail("got", strings.Join(payload.Fields, ","))
		}
	}

	return payload.Values, nil
}
