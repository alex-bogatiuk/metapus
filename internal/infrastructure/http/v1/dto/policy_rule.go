package dto

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/security_profile"
)

// --- PolicyRule DTOs ---

// PolicyRuleResponse is the API representation of a CEL policy rule.
type PolicyRuleResponse struct {
	ID          string    `json:"id"`
	ProfileID   string    `json:"profileId"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	EntityName  string    `json:"entityName"`
	Actions     []string  `json:"actions"`
	Expression  string    `json:"expression"`
	Effect      string    `json:"effect"`
	Priority    int       `json:"priority"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// FromPolicyRule converts domain PolicyRule to API response.
func FromPolicyRule(r *security_profile.PolicyRule) PolicyRuleResponse {
	return PolicyRuleResponse{
		ID:          r.ID.String(),
		ProfileID:   r.ProfileID.String(),
		Name:        r.Name,
		Description: r.Description,
		EntityName:  r.EntityName,
		Actions:     r.Actions,
		Expression:  r.Expression,
		Effect:      r.Effect,
		Priority:    r.Priority,
		Enabled:     r.Enabled,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// FromPolicyRules converts a slice of domain PolicyRules to API responses.
func FromPolicyRules(rules []*security_profile.PolicyRule) []PolicyRuleResponse {
	out := make([]PolicyRuleResponse, len(rules))
	for i, r := range rules {
		out[i] = FromPolicyRule(r)
	}
	return out
}

// CreatePolicyRuleRequest is the request body for creating a new policy rule.
type CreatePolicyRuleRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	EntityName  string   `json:"entityName" binding:"required"`
	Actions     []string `json:"actions" binding:"required,min=1"`
	Expression  string   `json:"expression" binding:"required"`
	Effect      string   `json:"effect" binding:"required,oneof=deny allow"`
	Priority    int      `json:"priority"`
	Enabled     *bool    `json:"enabled"`
}

// ToDomain converts the request to a domain PolicyRule.
func (r *CreatePolicyRuleRequest) ToDomain(profileID id.ID) *security_profile.PolicyRule {
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	return &security_profile.PolicyRule{
		ProfileID:   profileID,
		Name:        r.Name,
		Description: r.Description,
		EntityName:  r.EntityName,
		Actions:     r.Actions,
		Expression:  r.Expression,
		Effect:      r.Effect,
		Priority:    r.Priority,
		Enabled:     enabled,
	}
}

// UpdatePolicyRuleRequest is the request body for updating a policy rule.
type UpdatePolicyRuleRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	EntityName  *string  `json:"entityName"`
	Actions     []string `json:"actions"`
	Expression  *string  `json:"expression"`
	Effect      *string  `json:"effect" binding:"omitempty,oneof=deny allow"`
	Priority    *int     `json:"priority"`
	Enabled     *bool    `json:"enabled"`
}

// ApplyTo applies partial updates to an existing PolicyRule.
func (r *UpdatePolicyRuleRequest) ApplyTo(rule *security_profile.PolicyRule) {
	if r.Name != nil {
		rule.Name = *r.Name
	}
	if r.Description != nil {
		rule.Description = *r.Description
	}
	if r.EntityName != nil {
		rule.EntityName = *r.EntityName
	}
	if r.Actions != nil {
		rule.Actions = r.Actions
	}
	if r.Expression != nil {
		rule.Expression = *r.Expression
	}
	if r.Effect != nil {
		rule.Effect = *r.Effect
	}
	if r.Priority != nil {
		rule.Priority = *r.Priority
	}
	if r.Enabled != nil {
		rule.Enabled = *r.Enabled
	}
}

// ValidateExpressionRequest is the request body for validating a CEL expression.
type ValidateExpressionRequest struct {
	Expression string `json:"expression" binding:"required"`
}

// ValidateExpressionResponse is the response for CEL expression validation.
type ValidateExpressionResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// TestExpressionRequest is the request body for testing a CEL expression against sample data.
type TestExpressionRequest struct {
	Expression string         `json:"expression" binding:"required"`
	Doc        map[string]any `json:"doc"`
	Action     string         `json:"action"`
}

// TestExpressionResponse is the response for CEL expression testing.
type TestExpressionResponse struct {
	Result  bool   `json:"result"`
	Error   string `json:"error,omitempty"`
	Elapsed string `json:"elapsed"` // human-readable duration
}
