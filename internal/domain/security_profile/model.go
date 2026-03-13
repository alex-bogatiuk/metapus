// Package security_profile provides the SecurityProfile domain model.
// A SecurityProfile is a named set of RLS dimensions and FLS field policies
// assigned to users to control data visibility and field-level access.
package security_profile

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
)

// SecurityProfile is a named configuration of RLS + FLS rules.
// Administrators assign profiles to users; the middleware reads the
// effective profile and injects DataScope + FieldPolicies into context.
type SecurityProfile struct {
	ID          id.ID     `db:"id" json:"id"`
	Code        string    `db:"code" json:"code"`
	Name        string    `db:"name" json:"name"`
	Description string    `db:"description" json:"description,omitempty"`
	IsSystem    bool      `db:"is_system" json:"isSystem"`
	CreatedAt   time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt   time.Time `db:"updated_at" json:"updatedAt"`

	// RLS: dimension name → list of allowed entity IDs.
	// Example: {"organization": ["org-1","org-2"], "counterparty": ["cp-5"]}
	Dimensions map[string][]string `db:"-" json:"dimensions,omitempty"`

	// FLS: per-entity field policies.
	// Key format: "entity_name:action" (e.g. "goods_receipt:read").
	FieldPolicies map[string]*security.FieldPolicy `db:"-" json:"fieldPolicies,omitempty"`

	// CEL policy rules for fine-grained authorization.
	PolicyRules []*PolicyRule `db:"-" json:"policyRules,omitempty"`

	// UserCount is the number of users assigned to this profile (populated in List).
	UserCount int `db:"-" json:"-"`
}

// Validate performs domain-level validation (no DB access).
func (p *SecurityProfile) Validate(_ context.Context) error {
	if p.Code == "" {
		return apperror.NewValidation("code is required").
			WithDetail("field", "code")
	}
	if p.Name == "" {
		return apperror.NewValidation("name is required").
			WithDetail("field", "name")
	}
	return nil
}

// BuildDataScope converts profile dimensions into a DataScope struct.
// jwtOrgIDs are the organization IDs from JWT — they are intersected
// with profile dimensions for the "organization" dimension.
func (p *SecurityProfile) BuildDataScope(jwtOrgIDs []string, isAdmin bool) *security.DataScope {
	if isAdmin {
		return &security.DataScope{IsAdmin: true}
	}

	dims := make(map[string][]string, len(p.Dimensions)+1)

	// Copy profile dimensions
	for k, v := range p.Dimensions {
		dims[k] = v
	}

	// Organization dimension: intersect JWT orgs with profile orgs (if profile restricts them).
	// If profile has no "organization" dimension → use JWT orgs directly.
	if profileOrgs, ok := p.Dimensions[security.DimOrganization]; ok && len(profileOrgs) > 0 {
		dims[security.DimOrganization] = intersect(jwtOrgIDs, profileOrgs)
	} else {
		dims[security.DimOrganization] = jwtOrgIDs
	}

	return &security.DataScope{
		Dimensions: dims,
	}
}

// GetFieldPolicy returns the FieldPolicy for a given entity and action.
// Returns nil if no policy is defined (no restrictions).
func (p *SecurityProfile) GetFieldPolicy(entityName, action string) *security.FieldPolicy {
	if p == nil || p.FieldPolicies == nil {
		return nil
	}
	return p.FieldPolicies[entityName+":"+action]
}

// ─── Lightweight types for cross-domain queries ─────────────────────

// ProfileUser represents a user assigned to a security profile (for the Users tab).
type ProfileUser struct {
	ID        id.ID  `db:"id"`
	Email     string `db:"email"`
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
	IsActive  bool   `db:"is_active"`
}

// FullName returns user's display name.
func (u *ProfileUser) FullName() string {
	if u.FirstName == "" && u.LastName == "" {
		return u.Email
	}
	if u.LastName == "" {
		return u.FirstName
	}
	if u.FirstName == "" {
		return u.LastName
	}
	return u.FirstName + " " + u.LastName
}

// ProfileBrief is a lightweight profile reference for batch enrichment.
type ProfileBrief struct {
	ID   id.ID  `db:"id"`
	Code string `db:"code"`
	Name string `db:"name"`
}

// ─── Dimension DB row (for scanning) ────────────────────────────────

// DimensionRow represents a single row from security_profile_dimensions.
type DimensionRow struct {
	ID            id.ID    `db:"id"`
	ProfileID     id.ID    `db:"profile_id"`
	DimensionName string   `db:"dimension_name"`
	AllowedIDs    []string `db:"allowed_ids"`
}

// FieldPolicyRow represents a single row from security_profile_field_policies.
type FieldPolicyRow struct {
	ID            id.ID               `db:"id"`
	ProfileID     id.ID               `db:"profile_id"`
	EntityName    string              `db:"entity_name"`
	Action        string              `db:"action"`
	AllowedFields []string            `db:"allowed_fields"`
	TableParts    map[string][]string `db:"-"` // parsed from JSONB
	TablePartsRaw []byte              `db:"table_parts"`
}

// ─── helpers ─────────────────────────────────────────────────────────

// intersect returns elements present in both slices.
func intersect(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, v := range b {
		set[v] = struct{}{}
	}
	var result []string
	for _, v := range a {
		if _, ok := set[v]; ok {
			result = append(result, v)
		}
	}
	return result
}
