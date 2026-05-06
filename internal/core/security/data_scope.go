// Package security provides authorization and access control.
package security

import (
	"context"

	"github.com/Masterminds/squirrel"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
)

// DataScope defines row-level visibility and mutation boundaries for the current request.
//
// Instead of hardcoding specific columns (organization_id, counterparty_id),
// DataScope uses a generic Dimensions map where each dimension represents
// an access boundary (e.g., "organization", "counterparty", "cost_article").
//
// This allows adding new RLS dimensions without modifying the core:
//   - New catalog added (e.g., CostArticle) → register dimension in repo constructor
//   - User gets access to specific values → add to Dimensions map
//
// DataScope is created per-request and placed into context.
// Repository layer uses ApplyConditions to inject WHERE conditions;
// service layer uses CanAccessRecord / CanMutate for point-checks.
type DataScope struct {
	// IsAdmin bypasses all RLS checks.
	IsAdmin bool

	// Dimensions maps dimension names to allowed ID sets (global, all entities).
	// Key = dimension name (e.g., "organization", "counterparty", "cost_article").
	// Value = list of allowed IDs for that dimension.
	// Empty slice for a dimension means no access to any value in that dimension.
	// Missing dimension means no restriction on that dimension (all values allowed).
	Dimensions map[string][]string

	// EntityDimensions maps entity names to per-entity dimension overrides.
	// Key = entity_name (e.g., "goods_receipt"), value = dimension map.
	// Per-entity dimensions are merged with global Dimensions at query time.
	// Per-entity values override global values for the same dimension name.
	EntityDimensions map[string]map[string][]string

	// ReadOnly prevents any mutations (create/update/delete/post/unpost).
	ReadOnly bool
}

// Standard dimension names used across the system.
// New dimensions can be added by simply using new string constants.
const (
	DimOrganization = "organization"
	DimMerchant     = "merchant"
	// Future dimensions added via SecurityProfile (DB/cache):
	// DimCounterparty  = "counterparty"
	// DimCostArticle   = "cost_article"
	// DimDepartment    = "department"
	// DimProject       = "project"
)

// NewDataScopeFromContext creates DataScope from the current request context.
// This is a fallback used when SecurityContext middleware hasn't run.
// Returns an empty scope (fail-open: no dimensions = no restrictions)
// for authenticated users, or a restrictive scope for unauthenticated requests.
func NewDataScopeFromContext(ctx context.Context) *DataScope {
	user := appctx.GetUser(ctx)
	if user == nil {
		// Fail-closed: no user = no access
		return &DataScope{Dimensions: make(map[string][]string)}
	}

	// Fail-open: authenticated user without SecurityContext middleware
	// means no profile-based restrictions. Empty Dimensions = full access.
	return &DataScope{
		IsAdmin: user.IsAdmin,
	}
}

// SetDimension adds or replaces a dimension's allowed values.
func (ds *DataScope) SetDimension(name string, ids []string) {
	if ds.Dimensions == nil {
		ds.Dimensions = make(map[string][]string)
	}
	ds.Dimensions[name] = ids
}

// ApplyConditions returns squirrel WHERE conditions for RLS filtering.
//
// entityName identifies the current entity (e.g., "goods_receipt").
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
func (ds *DataScope) ApplyConditions(entityName string, dimColumns map[string]string) []squirrel.Sqlizer {
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
			conditions = append(conditions, squirrel.Eq{dbColumn: nil})
			continue
		}
		conditions = append(conditions, squirrel.Eq{dbColumn: allowedIDs})
	}

	return conditions
}

// EffectiveDimensions returns global dimensions merged with per-entity overrides.
// Per-entity values override global values for the same dimension name.
func (ds *DataScope) EffectiveDimensions(entityName string) map[string][]string {
	if ds == nil {
		return nil
	}

	// Start with global dimensions
	if len(ds.EntityDimensions) == 0 || entityName == "" {
		return ds.Dimensions
	}

	entityDims, hasEntity := ds.EntityDimensions[entityName]
	if !hasEntity || len(entityDims) == 0 {
		return ds.Dimensions
	}

	// Merge: copy global, override with per-entity
	merged := make(map[string][]string, len(ds.Dimensions)+len(entityDims))
	for k, v := range ds.Dimensions {
		merged[k] = v
	}
	for k, v := range entityDims {
		merged[k] = v
	}
	return merged
}

// CanAccessRecord checks if the current scope allows accessing a specific record.
//
// recordDimensions maps dimension names to the record's actual values.
// For each dimension present in BOTH the scope and recordDimensions,
// the record's value must be in the allowed set.
//
// Example:
//
//	scope.CanAccessRecord(map[string]string{
//	    "organization":  doc.OrganizationID.String(),
//	    "counterparty":  doc.CounterpartyID.String(),
//	})
//
// CanAccessRecord checks if the current scope allows accessing a specific record.
// entityName is used to merge per-entity dimension overrides.
func (ds *DataScope) CanAccessRecord(entityName string, recordDimensions map[string]string) bool {
	if ds == nil || ds.IsAdmin {
		return true
	}

	effective := ds.EffectiveDimensions(entityName)

	for dimName, recordValue := range recordDimensions {
		if recordValue == "" {
			// Record doesn't have this dimension value — skip
			continue
		}

		allowedIDs, hasDimension := effective[dimName]
		if !hasDimension {
			// Dimension not restricted in scope — skip
			continue
		}

		if len(allowedIDs) == 0 {
			// Dimension restricted to empty set — deny
			return false
		}

		found := false
		for _, id := range allowedIDs {
			if id == recordValue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// CanMutate returns an error if the scope is read-only.
func (ds *DataScope) CanMutate() error {
	if ds != nil && ds.ReadOnly {
		return apperror.NewForbidden("action not allowed by policy: read-only access")
	}
	return nil
}

// --- Context helpers ---

type dataScopeKey struct{}

// WithDataScope adds DataScope to context.
func WithDataScope(ctx context.Context, scope *DataScope) context.Context {
	return context.WithValue(ctx, dataScopeKey{}, scope)
}

// GetDataScope returns DataScope from context.
// If not found, creates one from UserContext (fail-closed).
func GetDataScope(ctx context.Context) *DataScope {
	if v, ok := ctx.Value(dataScopeKey{}).(*DataScope); ok {
		return v
	}
	return NewDataScopeFromContext(ctx)
}
