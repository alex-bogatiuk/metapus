// Package security provides authorization and access control.
package security

import (
	"context"

	"github.com/Masterminds/squirrel"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/apperror"
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

	// Dimensions maps dimension names to allowed ID sets.
	// Key = dimension name (e.g., "organization", "counterparty", "cost_article").
	// Value = list of allowed IDs for that dimension.
	// Empty slice for a dimension means no access to any value in that dimension.
	// Missing dimension means no restriction on that dimension (all values allowed).
	Dimensions map[string][]string

	// ReadOnly prevents any mutations (create/update/delete/post/unpost).
	ReadOnly bool
}

// Standard dimension names used across the system.
// New dimensions can be added by simply using new string constants.
const (
	DimOrganization = "organization"
	// Future dimensions added via SecurityProfile (DB/cache):
	// DimCounterparty  = "counterparty"
	// DimCostArticle   = "cost_article"
	// DimDepartment    = "department"
	// DimProject       = "project"
)

// NewDataScopeFromContext creates DataScope from the current request context.
// Maps UserContext fields to standard dimensions for backward compatibility.
// Returns a restrictive (no access) scope if user context is missing.
func NewDataScopeFromContext(ctx context.Context) *DataScope {
	user := appctx.GetUser(ctx)
	if user == nil {
		// Fail-closed: no user = no access
		return &DataScope{Dimensions: make(map[string][]string)}
	}

	dims := make(map[string][]string)

	if len(user.OrgIDs) > 0 {
		dims[DimOrganization] = user.OrgIDs
	}
	// Additional dimensions (counterparty, cost_article, etc.) will be
	// loaded from SecurityProfile when it is implemented.
	// SecurityProfile is loaded from DB per-user and cached.

	return &DataScope{
		IsAdmin:    user.IsAdmin,
		Dimensions: dims,
		ReadOnly:   false, // determined by role/policy, not user context alone
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
// dimColumns maps dimension names to DB column names for the current entity.
// For each dimension present in both DataScope.Dimensions and dimColumns,
// a WHERE column IN (...) condition is generated.
//
// Example dimColumns:
//
//	{"organization": "organization_id", "counterparty": "supplier_id"}
//
// If DataScope has Dimensions["organization"] = ["org-1","org-2"], this produces:
//
//	WHERE organization_id IN ('org-1','org-2')
func (ds *DataScope) ApplyConditions(dimColumns map[string]string) []squirrel.Sqlizer {
	if ds == nil || ds.IsAdmin || len(dimColumns) == 0 {
		return nil
	}

	var conditions []squirrel.Sqlizer

	for dimName, dbColumn := range dimColumns {
		allowedIDs, hasDimension := ds.Dimensions[dimName]
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
//	    "counterparty":  doc.SupplierID.String(),
//	})
func (ds *DataScope) CanAccessRecord(recordDimensions map[string]string) bool {
	if ds == nil || ds.IsAdmin {
		return true
	}

	for dimName, recordValue := range recordDimensions {
		if recordValue == "" {
			// Record doesn't have this dimension value — skip
			continue
		}

		allowedIDs, hasDimension := ds.Dimensions[dimName]
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
