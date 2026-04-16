package automations

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// AutomationRule defines a trigger and action template.
type AutomationRule struct {
	ID               id.ID      `json:"id"`
	Name             string     `json:"name"`
	OrganizationID   *id.ID     `json:"organizationId,omitempty"`
	EventType        string     `json:"eventType"`
	ConditionCEL     *string    `json:"conditionCel,omitempty"`
	ActionType       string     `json:"actionType"`
	ActionTemplate   string     `json:"actionTemplate"`
	ServiceAccountID *id.ID     `json:"serviceAccountId,omitempty"`
	IsActive         bool       `json:"isActive"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

// CreateRuleRequest encapsulates the data needed to create a new rule.
type CreateRuleRequest struct {
	Name             string  `json:"name"`
	OrganizationID   *id.ID  `json:"organizationId,omitempty"`
	EventType        string  `json:"eventType"`
	ConditionCEL     *string `json:"conditionCel,omitempty"`
	ActionType       string  `json:"actionType"`
	ActionTemplate   string  `json:"actionTemplate"`
	ServiceAccountID *id.ID  `json:"serviceAccountId,omitempty"`
	IsActive         bool    `json:"isActive"`
}

// UpdateRuleRequest encapsulates the data needed to update an existing rule.
type UpdateRuleRequest struct {
	Name             string  `json:"name"`
	OrganizationID   *id.ID  `json:"organizationId,omitempty"`
	EventType        string  `json:"eventType"`
	ConditionCEL     *string `json:"conditionCel,omitempty"`
	ActionType       string  `json:"actionType"`
	ActionTemplate   string  `json:"actionTemplate"`
	ServiceAccountID *id.ID  `json:"serviceAccountId,omitempty"`
	IsActive         bool    `json:"isActive"`
}

// Validate checks if the CreateRuleRequest is valid.
func (r *CreateRuleRequest) Validate(ctx context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if r.EventType == "" {
		return apperror.NewValidation("event type is required").WithDetail("field", "eventType")
	}
	if r.ActionType == "" {
		return apperror.NewValidation("action type is required").WithDetail("field", "actionType")
	}
	if r.ActionType != "internal_notification" && r.ServiceAccountID == nil {
		return apperror.NewValidation("service account id is required").WithDetail("field", "serviceAccountId")
	}
	// Note: We don't strictly validate CEL syntax here; that requires the CEL engine.
	// But in a more robust setup, we could compile it here to test validity before save.
	return nil
}

// Validate checks if the UpdateRuleRequest is valid.
func (r *UpdateRuleRequest) Validate(ctx context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if r.EventType == "" {
		return apperror.NewValidation("event type is required").WithDetail("field", "eventType")
	}
	if r.ActionType == "" {
		return apperror.NewValidation("action type is required").WithDetail("field", "actionType")
	}
	if r.ActionType != "internal_notification" && r.ServiceAccountID == nil {
		return apperror.NewValidation("service account id is required").WithDetail("field", "serviceAccountId")
	}
	return nil
}

// TestRuleRequest encapsulates the data needed to test an automation rule.
type TestRuleRequest struct {
	ConditionCEL   *string        `json:"conditionCel,omitempty"`
	ActionTemplate string         `json:"actionTemplate"`
	Payload        map[string]any `json:"payload"`
}

// TestRuleResponse encapsulates the result of evaluating a test rule.
type TestRuleResponse struct {
	ConditionMatched bool   `json:"conditionMatched"`
	ConditionError   string `json:"conditionError,omitempty"`
	RenderedPayload  string `json:"renderedPayload,omitempty"`
	RenderError      string `json:"renderError,omitempty"`
}
