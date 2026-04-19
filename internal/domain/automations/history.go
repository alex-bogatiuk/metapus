package automations

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// HistoryStatus represents the outcome of a rule evaluation or delivery.
type HistoryStatus string

const (
	HistorySuccess        HistoryStatus = "success"
	HistoryError          HistoryStatus = "error"
	HistoryConditionFalse HistoryStatus = "condition_false"
	HistorySkipped        HistoryStatus = "skipped" // e.g. cooldown
	HistoryPending        HistoryStatus = "pending"
)

// HistoryEntry records the result of an automation rule evaluation or delivery.
// Denormalized for fast UI queries without JOINs.
type HistoryEntry struct {
	ID              id.ID         `json:"id"`
	RuleID          id.ID         `json:"ruleId"`
	RuleName        string        `json:"ruleName"`
	EventType       string        `json:"eventType"`
	AggregateID     *id.ID        `json:"aggregateId,omitempty"`
	AggregateName   *string       `json:"aggregateName,omitempty"`
	Status          HistoryStatus `json:"status"`
	ChannelID       *id.ID        `json:"channelId,omitempty"`
	ChannelName     *string       `json:"channelName,omitempty"`
	AccountName     *string       `json:"accountName,omitempty"`
	RenderedPayload *string       `json:"renderedPayload,omitempty"`
	ErrorText       *string       `json:"errorText,omitempty"`
	DurationMs      *int          `json:"durationMs,omitempty"`
	CreatedAt       time.Time     `json:"createdAt"`
}

// HistoryFilter defines filter criteria for querying history.
type HistoryFilter struct {
	RuleID    *id.ID
	Status    *HistoryStatus
	ChannelID *id.ID
	From      *time.Time
	To        *time.Time
	Limit     int
	Offset    int
}

// HistoryRepository provides access to rule execution logs.
type HistoryRepository interface {
	// Create saves a new history entry (append-only).
	Create(ctx context.Context, entry *HistoryEntry) error

	// List returns filtered and paginated history entries.
	List(ctx context.Context, filter HistoryFilter) ([]HistoryEntry, int, error)

	// GetByID retrieves a single history entry by ID.
	GetByID(ctx context.Context, entryID id.ID) (*HistoryEntry, error)
}
