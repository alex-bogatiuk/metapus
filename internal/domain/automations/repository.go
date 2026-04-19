package automations

import (
	"context"

	"metapus/internal/core/id"
)

// AccountRepository provides data access for automation accounts.
type AccountRepository interface {
	List(ctx context.Context) ([]Account, error)
	GetByID(ctx context.Context, accountID id.ID) (*Account, error)
	Create(ctx context.Context, req CreateAccountRequest) (*Account, error)
	Update(ctx context.Context, accountID id.ID, req UpdateAccountRequest) (*Account, error)
	Delete(ctx context.Context, accountID id.ID) error

	// UpdateLastResult updates the status/last_error/last_success_at fields.
	// Called by the Engine after delivery attempt.
	UpdateLastResult(ctx context.Context, accountID id.ID, success bool, lastErr *string) error
}

// ChannelRepository provides data access for automation channels.
type ChannelRepository interface {
	List(ctx context.Context, accountID *id.ID) ([]Channel, error)
	GetByID(ctx context.Context, channelID id.ID) (*Channel, error)
	Create(ctx context.Context, req CreateChannelRequest) (*Channel, error)
	Update(ctx context.Context, channelID id.ID, req UpdateChannelRequest) (*Channel, error)
	Delete(ctx context.Context, channelID id.ID) error
}

// RuleRepository provides data access for automation rules.
type RuleRepository interface {
	List(ctx context.Context, eventType *string) ([]Rule, error)

	// ListActiveByEvent is the hot path for Engine.Evaluate.
	// Matches rules by event_type AND (target_entities contains entityName OR is NULL/wildcard).
	ListActiveByEvent(ctx context.Context, eventType string, entityName string) ([]Rule, error)

	// ListActiveByTriggerType returns active rules by trigger type (e.g. "scheduled").
	ListActiveByTriggerType(ctx context.Context, triggerType TriggerType) ([]Rule, error)

	GetByID(ctx context.Context, ruleID id.ID) (*Rule, error)

	// Create creates a rule and its subscribers in a single transaction.
	Create(ctx context.Context, req CreateRuleRequest) (*Rule, error)

	// Update updates a rule and replaces its subscribers (delete+insert) atomically.
	Update(ctx context.Context, ruleID id.ID, req UpdateRuleRequest) (*Rule, error)

	Delete(ctx context.Context, ruleID id.ID) error

	// Toggle switches is_active flag (used for quick toggle from UI list).
	Toggle(ctx context.Context, ruleID id.ID) (bool, error)

	// IncrementStats atomically increments execution_count (and error_count if isError).
	IncrementStats(ctx context.Context, ruleID id.ID, isError bool) error
}

// SubscriberRepository provides data access for rule subscribers.
type SubscriberRepository interface {
	// ListByRuleID returns all subscribers for a given rule.
	ListByRuleID(ctx context.Context, ruleID id.ID) ([]Subscriber, error)

	// ReplaceForRule deletes all existing subscribers for a rule and inserts new ones.
	// Must be called within a transaction (managed by RuleRepository.Update).
	ReplaceForRule(ctx context.Context, ruleID id.ID, subs []SubscriberInput) ([]Subscriber, error)
}

// CredentialManager handles writing and reading encrypted credentials.
// Separated from AccountRepository to emphasize the distinct security lifecycle.
type CredentialManager interface {
	// WriteCredentials sets or updates encrypted credentials for an account.
	WriteCredentials(ctx context.Context, accountID id.ID, credentials []byte) error

	// ReadCredentials retrieves and decrypts credentials for an account.
	// ONLY used by the Automation Engine — never exposed via API.
	ReadCredentials(ctx context.Context, accountID id.ID) ([]byte, error)
}
