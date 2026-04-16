package automations

import (
	"context"

	"metapus/internal/core/id"
)

// Repository provides data access for automation rules.
type Repository interface {
	// List returns all automation rules, optionally filtered by event type.
	List(ctx context.Context, eventType *string) ([]AutomationRule, error)

	// ListActiveByEventType returns all *active* rules that match the event type.
	// Used heavily by the Automation Engine worker.
	ListActiveByEventType(ctx context.Context, eventType string) ([]AutomationRule, error)

	// GetByID retrieves a rule by ID.
	GetByID(ctx context.Context, ruleID id.ID) (*AutomationRule, error)

	// Create creates a new rule.
	Create(ctx context.Context, req CreateRuleRequest) (*AutomationRule, error)

	// Update modifies an existing rule.
	Update(ctx context.Context, ruleID id.ID, req UpdateRuleRequest) (*AutomationRule, error)

	// Delete removes a rule.
	Delete(ctx context.Context, ruleID id.ID) error
}
