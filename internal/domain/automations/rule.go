package automations

import (
	"context"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// TriggerType defines what initiates the rule evaluation.
type TriggerType string

const (
	TriggerEntityEvent   TriggerType = "entity_event"
	TriggerBusinessEvent TriggerType = "business_event"
	TriggerScheduled     TriggerType = "scheduled"
	TriggerWebhook       TriggerType = "incoming_webhook"
)

// ReactionType defines the reaction on a matched rule.
type ReactionType string

const (
	ReactionNotify         ReactionType = "notify"
	ReactionWebhookCall    ReactionType = "webhook_call"
	ReactionChain          ReactionType = "chain"
	ReactionCreateRecord   ReactionType = "create_record"
	ReactionGenerateReport ReactionType = "generate_report"
)

// Rule defines an automation rule: event → condition → reaction.
type Rule struct {
	ID             id.ID        `json:"id"`
	Name           string       `json:"name"`
	Description    *string      `json:"description,omitempty"`
	TriggerType    TriggerType  `json:"triggerType"`
	EventType      string       `json:"eventType"`
	TargetEntities []string     `json:"targetEntities"`
	ConditionCEL   *string      `json:"conditionCel,omitempty"`
	ReactionType   ReactionType `json:"reactionType"`
	NotifSeverity  string       `json:"notifSeverity,omitempty"`
	MessageFormat  string       `json:"messageFormat"`
	ActionTemplate string       `json:"actionTemplate"`
	ChainRuleIDs   []id.ID             `json:"chainRuleIds,omitempty"`
	ReportConfig   *ReportActionConfig `json:"reportConfig,omitempty"`
	Priority       int                 `json:"priority"`
	MaxRetries     int          `json:"maxRetries"`
	CooldownSecs   int          `json:"cooldownSeconds"`
	OrganizationID *id.ID       `json:"organizationId,omitempty"`
	IsActive       bool         `json:"isActive"`
	ExecutionCount int          `json:"executionCount"`
	ErrorCount     int          `json:"errorCount"`
	LastExecutedAt *time.Time   `json:"lastExecutedAt,omitempty"`
	DeletionMark   bool         `json:"deletionMark"`
	Version        int          `json:"version"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`

	// Table part: subscribers (loaded inline with the rule)
	Subscribers []Subscriber `json:"subscribers"`
}

// CreateRuleRequest encapsulates data for creating a new rule.
type CreateRuleRequest struct {
	Name           string            `json:"name"`
	Description    *string           `json:"description,omitempty"`
	TriggerType    TriggerType       `json:"triggerType"`
	EventType      string            `json:"eventType"`
	TargetEntities []string          `json:"targetEntities"`
	ConditionCEL   *string           `json:"conditionCel,omitempty"`
	ReactionType   ReactionType      `json:"reactionType"`
	NotifSeverity  string            `json:"notifSeverity,omitempty"`
	MessageFormat  string            `json:"messageFormat"`
	ActionTemplate string            `json:"actionTemplate"`
	ChainRuleIDs   []id.ID              `json:"chainRuleIds,omitempty"`
	ReportConfig   *ReportActionConfig  `json:"reportConfig,omitempty"`
	Priority       int                  `json:"priority"`
	MaxRetries     int               `json:"maxRetries"`
	CooldownSecs   int               `json:"cooldownSeconds"`
	OrganizationID *id.ID            `json:"organizationId,omitempty"`
	IsActive       bool              `json:"isActive"`
	Subscribers    []SubscriberInput `json:"subscribers"`
}

// Validate checks if the CreateRuleRequest is valid.
func (r *CreateRuleRequest) Validate(_ context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if r.EventType == "" {
		return apperror.NewValidation("event type is required").WithDetail("field", "eventType")
	}

	switch r.TriggerType {
	case TriggerEntityEvent, TriggerBusinessEvent, TriggerScheduled, TriggerWebhook:
		// OK
	default:
		return apperror.NewValidation("invalid trigger type").WithDetail("triggerType", string(r.TriggerType))
	}

	switch r.ReactionType {
	case ReactionNotify, ReactionWebhookCall, ReactionChain, ReactionCreateRecord, ReactionGenerateReport:
		// OK
	default:
		return apperror.NewValidation("invalid reaction type").WithDetail("reactionType", string(r.ReactionType))
	}

	// Validate event_type is a known action for entity_event triggers
	if r.TriggerType == TriggerEntityEvent {
		switch r.EventType {
		case "posted", "unposted", "created", "updated", "deleted", "deletion_marked", "deletion_cleared":
			// OK — known entity actions
		default:
			return apperror.NewValidation("unknown entity event action: " + r.EventType).
				WithDetail("field", "eventType")
		}
	}

	if r.TriggerType == TriggerScheduled && !strings.HasPrefix(r.EventType, "cron:") {
		return apperror.NewValidation("scheduled trigger event_type must start with 'cron:'").
			WithDetail("field", "eventType")
	}

	// Validate CRON expression syntax at save time (not just at runtime in Scheduler)
	if r.TriggerType == TriggerScheduled {
		cronExpr := strings.TrimPrefix(r.EventType, "cron:")
		parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(cronExpr); err != nil {
			return apperror.NewValidation("invalid cron expression: " + err.Error()).
				WithDetail("field", "eventType")
		}
	}

	if r.ReactionType == ReactionChain && len(r.ChainRuleIDs) == 0 {
		return apperror.NewValidation("chain reaction requires at least one chain_rule_id").
			WithDetail("field", "chainRuleIds")
	}

	if r.ReactionType == ReactionGenerateReport {
		if r.ReportConfig == nil {
			return apperror.NewValidation("generate_report reaction requires report_config").
				WithDetail("field", "reportConfig")
		}
		if err := r.ReportConfig.Validate(); err != nil {
			return err
		}
	}

	if r.MessageFormat == "" {
		r.MessageFormat = "text"
	}
	if r.MaxRetries == 0 {
		r.MaxRetries = 3
	}

	for i := range r.Subscribers {
		if err := r.Subscribers[i].Validate(); err != nil {
			return err
		}
	}

	return nil
}

// UpdateRuleRequest encapsulates data for updating an existing rule.
type UpdateRuleRequest struct {
	Name           string            `json:"name"`
	Description    *string           `json:"description,omitempty"`
	TriggerType    TriggerType       `json:"triggerType"`
	EventType      string            `json:"eventType"`
	TargetEntities []string          `json:"targetEntities"`
	ConditionCEL   *string           `json:"conditionCel,omitempty"`
	ReactionType   ReactionType         `json:"reactionType"`
	NotifSeverity  string               `json:"notifSeverity,omitempty"`
	MessageFormat  string               `json:"messageFormat"`
	ActionTemplate string               `json:"actionTemplate"`
	ChainRuleIDs   []id.ID              `json:"chainRuleIds,omitempty"`
	ReportConfig   *ReportActionConfig  `json:"reportConfig,omitempty"`
	Priority       int                  `json:"priority"`
	MaxRetries     int               `json:"maxRetries"`
	CooldownSecs   int               `json:"cooldownSeconds"`
	OrganizationID *id.ID            `json:"organizationId,omitempty"`
	IsActive       bool              `json:"isActive"`
	Version        int               `json:"version"` // Optimistic Locking
	Subscribers    []SubscriberInput `json:"subscribers"`
}

// Validate checks if the UpdateRuleRequest is valid.
func (r *UpdateRuleRequest) Validate(_ context.Context) error {
	if r.Name == "" {
		return apperror.NewValidation("name is required").WithDetail("field", "name")
	}
	if r.EventType == "" {
		return apperror.NewValidation("event type is required").WithDetail("field", "eventType")
	}
	if r.Version < 1 {
		return apperror.NewValidation("version is required for optimistic locking").WithDetail("field", "version")
	}

	switch r.TriggerType {
	case TriggerEntityEvent, TriggerBusinessEvent, TriggerScheduled, TriggerWebhook:
	default:
		return apperror.NewValidation("invalid trigger type").WithDetail("triggerType", string(r.TriggerType))
	}

	switch r.ReactionType {
	case ReactionNotify, ReactionWebhookCall, ReactionChain, ReactionCreateRecord, ReactionGenerateReport:
	default:
		return apperror.NewValidation("invalid reaction type").WithDetail("reactionType", string(r.ReactionType))
	}

	// Validate event_type is a known action for entity_event triggers
	if r.TriggerType == TriggerEntityEvent {
		switch r.EventType {
		case "posted", "unposted", "created", "updated", "deleted", "deletion_marked", "deletion_cleared":
		default:
			return apperror.NewValidation("unknown entity event action: " + r.EventType).
				WithDetail("field", "eventType")
		}
	}

	// Validate CRON expression syntax for scheduled triggers
	if r.TriggerType == TriggerScheduled {
		if !strings.HasPrefix(r.EventType, "cron:") {
			return apperror.NewValidation("scheduled trigger event_type must start with 'cron:'").
				WithDetail("field", "eventType")
		}
		cronExpr := strings.TrimPrefix(r.EventType, "cron:")
		parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(cronExpr); err != nil {
			return apperror.NewValidation("invalid cron expression: " + err.Error()).
				WithDetail("field", "eventType")
		}
	}

	for i := range r.Subscribers {
		if err := r.Subscribers[i].Validate(); err != nil {
			return err
		}
	}

	if r.ReactionType == ReactionGenerateReport {
		if r.ReportConfig == nil {
			return apperror.NewValidation("generate_report reaction requires report_config").
				WithDetail("field", "reportConfig")
		}
		if err := r.ReportConfig.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// TestRuleRequest encapsulates data for testing a rule (dry-run).
type TestRuleRequest struct {
	ConditionCEL   *string        `json:"conditionCel,omitempty"`
	ActionTemplate string         `json:"actionTemplate"`
	Payload        map[string]any `json:"payload"`
}

// TestRuleResponse encapsulates the test result.
type TestRuleResponse struct {
	ConditionMatched bool   `json:"conditionMatched"`
	ConditionError   string `json:"conditionError,omitempty"`
	RenderedPayload  string `json:"renderedPayload,omitempty"`
	RenderError      string `json:"renderError,omitempty"`
}
