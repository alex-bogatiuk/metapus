package automations

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// ExecutionHistory records the result of an automation rule execution.
// It is used for observability and debugging, allowing admins to see why
// a webhook failed or what exact payload was sent.
type ExecutionHistory struct {
	ID             id.ID      `json:"id"`
	RuleID         id.ID      `json:"ruleId"`
	EventType      string     `json:"eventType"`
	AggregateID    id.ID      `json:"aggregateId"`
	Success        bool       `json:"success"`
	ErrorMessage   *string    `json:"errorMessage"`
	RequestPayload *string    `json:"requestPayload"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// HistoryRepository provides access to rule execution logs.
type HistoryRepository interface {
	// Create saves a new history record. This is typically fire-and-forget from the worker.
	Create(ctx context.Context, history *ExecutionHistory) error

	// ListByRuleID returns recent execution history for a specific rule.
	ListByRuleID(ctx context.Context, ruleID id.ID, limit int) ([]ExecutionHistory, error)
}
