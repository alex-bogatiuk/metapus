// Package filter builds SQL conditions of list filters.
package filter

import (
	"github.com/Masterminds/squirrel"

	"metapus/internal/domain/filter"
	core "metapus/internal/infrastructure/storage/postgres/filter"
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
	return core.BuildSearchConditions(query, searchCols)
}

// BuildSingleCondition translates a single resolved filter Item into a squirrel.Sqlizer condition.
//
// sqlExpr is the fully resolved SQL expression for the field, e.g.:
//   - "base.merchant_id"            (direct column)
//   - "j1.name"                     (joined reference attribute)
//   - "DATE(base.date)"             (date-wrapped)
func BuildSingleCondition(item filter.ResolvedItem, sqlExpr string) (squirrel.Sqlizer, error) {
	return core.BuildSingleCondition(item, sqlExpr)
}
