// Package keyset builds keyset pagination queries.
package keyset

import (
	"github.com/Masterminds/squirrel"

	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/keyset"
)

func BuildCursorFromRow(p0 core.SortSpec, sortValue any, idValue any) (string, error) {
	return core.BuildCursorFromRow(p0, sortValue, idValue)
}

func DecodeCursor(token string, p1 core.SortSpec) ([]any, error) { return core.DecodeCursor(token, p1) }

// ParseOrderBy parses an orderBy string like "-date" or "name" into a SortSpec.
// Returns the field and direction. Validates the field against allowed columns.
func ParseOrderBy(orderBy string, defaultField string, defaultDir string, allowedCols map[string]struct{}) (core.SortSpec, error) {
	return core.ParseOrderBy(orderBy, defaultField, defaultDir, allowedCols)
}

func TupleCondition(p0 core.SortSpec, values []any, forward bool) squirrel.Sqlizer {
	return core.TupleCondition(p0, values, forward)
}

func TupleConditionInclusive(p0 core.SortSpec, values []any, forward bool) squirrel.Sqlizer {
	return core.TupleConditionInclusive(p0, values, forward)
}
