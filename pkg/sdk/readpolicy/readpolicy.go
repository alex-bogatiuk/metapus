// Package readpolicy applies the read constraints of a security profile to
// dataset queries.
package readpolicy

import (
	"github.com/Masterminds/squirrel"

	"github.com/alex-bogatiuk/metapus/internal/core/security"
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/readpolicy"
)

type (
	DatasetExecutor = core.DatasetExecutor
	// ReadConstraints contains complete application-authorized source builders.
	// Each builder already contains FROM, source policies, and secured joins. An
	// executor can only add its business filters and aggregates to these builders.
	ReadConstraints = core.ReadConstraints
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
	return core.ApplyConditions(ds, entityName, dimColumns)
}
