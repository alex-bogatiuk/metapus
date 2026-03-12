package dto

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/security_profile"
)

// --- SecurityProfile Response DTOs ---

// SecurityProfileResponse is the API representation of a security profile.
type SecurityProfileResponse struct {
	ID            string                `json:"id"`
	Code          string                `json:"code"`
	Name          string                `json:"name"`
	Description   string                `json:"description,omitempty"`
	IsSystem      bool                  `json:"isSystem"`
	CreatedAt     time.Time             `json:"createdAt"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	Dimensions    map[string][]string   `json:"dimensions,omitempty"`
	FieldPolicies []FieldPolicyResponse `json:"fieldPolicies,omitempty"`
	PolicyRules   []PolicyRuleResponse  `json:"policyRules,omitempty"`
}

// FieldPolicyResponse is the API representation of a field policy.
type FieldPolicyResponse struct {
	EntityName    string              `json:"entityName"`
	Action        string              `json:"action"`
	AllowedFields []string            `json:"allowedFields"`
	TableParts    map[string][]string `json:"tableParts,omitempty"`
}

// FromSecurityProfile converts domain SecurityProfile to API response.
func FromSecurityProfile(p *security_profile.SecurityProfile) SecurityProfileResponse {
	resp := SecurityProfileResponse{
		ID:          p.ID.String(),
		Code:        p.Code,
		Name:        p.Name,
		Description: p.Description,
		IsSystem:    p.IsSystem,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
		Dimensions:  p.Dimensions,
	}

	// Convert field policies map to slice
	if p.FieldPolicies != nil {
		fps := make([]FieldPolicyResponse, 0, len(p.FieldPolicies))
		for _, fp := range p.FieldPolicies {
			fps = append(fps, FieldPolicyResponse{
				EntityName:    fp.EntityName,
				Action:        fp.Action,
				AllowedFields: fp.AllowedFields,
				TableParts:    fp.TableParts,
			})
		}
		resp.FieldPolicies = fps
	}

	// Convert policy rules
	if p.PolicyRules != nil {
		resp.PolicyRules = FromPolicyRules(p.PolicyRules)
	}

	return resp
}

// FromSecurityProfiles converts a slice of domain SecurityProfiles to API responses.
func FromSecurityProfiles(profiles []*security_profile.SecurityProfile) []SecurityProfileResponse {
	out := make([]SecurityProfileResponse, len(profiles))
	for i, p := range profiles {
		out[i] = FromSecurityProfile(p)
	}
	return out
}

// SecurityProfileListItem is a lightweight response for list views.
type SecurityProfileListItem struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	IsSystem    bool      `json:"isSystem"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// FromSecurityProfileListItem converts domain SecurityProfile to lightweight list item.
func FromSecurityProfileListItem(p *security_profile.SecurityProfile) SecurityProfileListItem {
	return SecurityProfileListItem{
		ID:          p.ID.String(),
		Code:        p.Code,
		Name:        p.Name,
		Description: p.Description,
		IsSystem:    p.IsSystem,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// --- SecurityProfile Request DTOs ---

// CreateSecurityProfileRequest is the request body for creating a security profile.
type CreateSecurityProfileRequest struct {
	Code          string              `json:"code" binding:"required"`
	Name          string              `json:"name" binding:"required"`
	Description   string              `json:"description"`
	Dimensions    map[string][]string `json:"dimensions"`
	FieldPolicies []FieldPolicyInput  `json:"fieldPolicies"`
}

// FieldPolicyInput is the input for a field policy.
type FieldPolicyInput struct {
	EntityName    string              `json:"entityName" binding:"required"`
	Action        string              `json:"action" binding:"required"`
	AllowedFields []string            `json:"allowedFields" binding:"required"`
	TableParts    map[string][]string `json:"tableParts,omitempty"`
}

// ToDomain converts the request to a domain SecurityProfile.
func (r *CreateSecurityProfileRequest) ToDomain() *security_profile.SecurityProfile {
	profile := &security_profile.SecurityProfile{
		Code:        r.Code,
		Name:        r.Name,
		Description: r.Description,
		Dimensions:  r.Dimensions,
	}

	if len(r.FieldPolicies) > 0 {
		fps := make(map[string]*security.FieldPolicy, len(r.FieldPolicies))
		for _, fp := range r.FieldPolicies {
			key := fp.EntityName + ":" + fp.Action
			fps[key] = &security.FieldPolicy{
				EntityName:    fp.EntityName,
				Action:        fp.Action,
				AllowedFields: fp.AllowedFields,
				TableParts:    fp.TableParts,
			}
		}
		profile.FieldPolicies = fps
	}

	return profile
}

// UpdateSecurityProfileRequest is the request body for updating a security profile.
type UpdateSecurityProfileRequest struct {
	Code          *string             `json:"code"`
	Name          *string             `json:"name"`
	Description   *string             `json:"description"`
	Dimensions    map[string][]string `json:"dimensions"`
	FieldPolicies []FieldPolicyInput  `json:"fieldPolicies"`
}

// ApplyTo applies partial updates to an existing SecurityProfile.
func (r *UpdateSecurityProfileRequest) ApplyTo(profile *security_profile.SecurityProfile) {
	if r.Code != nil {
		profile.Code = *r.Code
	}
	if r.Name != nil {
		profile.Name = *r.Name
	}
	if r.Description != nil {
		profile.Description = *r.Description
	}
	if r.Dimensions != nil {
		profile.Dimensions = r.Dimensions
	}
	if r.FieldPolicies != nil {
		fps := make(map[string]*security.FieldPolicy, len(r.FieldPolicies))
		for _, fp := range r.FieldPolicies {
			key := fp.EntityName + ":" + fp.Action
			fps[key] = &security.FieldPolicy{
				EntityName:    fp.EntityName,
				Action:        fp.Action,
				AllowedFields: fp.AllowedFields,
				TableParts:    fp.TableParts,
			}
		}
		profile.FieldPolicies = fps
	}
}

// AssignProfileRequest is the request for assigning a user to a security profile.
type AssignProfileRequest struct {
	UserID string `json:"userId" binding:"required,uuid"`
}

// --- User list response for admin ---

// UserListItem represents a user in the admin user list.
type UserListItem struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	FirstName string    `json:"firstName,omitempty"`
	LastName  string    `json:"lastName,omitempty"`
	FullName  string    `json:"fullName"`
	IsActive  bool      `json:"isActive"`
	IsAdmin   bool      `json:"isAdmin"`
	Roles     []string  `json:"roles"`
	ProfileID *string   `json:"profileId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ProfileUserItem represents a user assigned to a specific profile.
type ProfileUserItem struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	FullName string `json:"fullName"`
}

// --- Audit log response ---

// AuditEntryResponse is the API representation of a single audit log entry.
type AuditEntryResponse struct {
	ID        string         `json:"id"`
	Action    string         `json:"action"`
	UserID    string         `json:"userId,omitempty"`
	UserEmail string         `json:"userEmail,omitempty"`
	Changes   map[string]any `json:"changes,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

// --- Helper to parse ID ---

func parseProfileID(raw string) (id.ID, error) {
	return id.Parse(raw)
}
