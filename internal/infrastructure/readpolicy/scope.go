package readpolicy

import (
	"github.com/Masterminds/squirrel"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
)

// ApplyConditions returns squirrel WHERE conditions for RLS filtering.
//
// entityName identifies the current entity (e.g., "crypto_invoice").
// dimColumns maps dimension names to DB column names for the current entity.
// For each dimension present in both the effective dimensions and dimColumns,
// a WHERE column IN (...) condition is generated.
//
// Effective dimensions = global Dimensions merged with EntityDimensions[entityName].
// Per-entity values override global values for the same dimension name.
//
// Example dimColumns:
//
//	{"organization": "organization_id", "counterparty": "counterparty_id"}
//
// If effective dimensions have "organization" = ["org-1","org-2"], this produces:
//
//	WHERE organization_id IN ('org-1','org-2')
func ApplyConditions(ds *security.DataScope, entityName string, dimColumns map[string]string) []squirrel.Sqlizer {
	if ds == nil || ds.IsAdmin || len(dimColumns) == 0 {
		return nil
	}

	// Build effective dimensions: global + per-entity overrides
	effective := ds.EffectiveDimensions(entityName)

	var conditions []squirrel.Sqlizer

	for dimName, dbColumn := range dimColumns {
		allowedIDs, hasDimension := effective[dimName]
		if !hasDimension {
			// Dimension not in scope → no restriction on this dimension
			continue
		}
		if len(allowedIDs) == 0 {
			// Dimension present but empty → no access (fail-closed)
			// Use impossible condition to guarantee zero results
			conditions = append(conditions, squirrel.Expr("FALSE"))
			continue
		}
		conditions = append(conditions, squirrel.Eq{dbColumn: allowedIDs})
	}

	return conditions
}
