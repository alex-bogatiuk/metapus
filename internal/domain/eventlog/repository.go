package eventlog

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/cursor"
)

// Writer records events into the event log.
type Writer interface {
	// Write records a single event.
	Write(ctx context.Context, event Event) error

	// WriteBatch records multiple events in one operation.
	WriteBatch(ctx context.Context, events []Event) error
}

// ListResult contains cursor-paginated event log results.
type ListResult struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
	PrevCursor string  `json:"prevCursor,omitempty"`
	HasMore    bool    `json:"hasMore"`
	HasPrev    bool    `json:"hasPrev"`
	TotalCount int64   `json:"totalCount"`
}

// Reader retrieves events from the event log.
type Reader interface {
	// List returns events matching the filter with cursor pagination.
	List(ctx context.Context, filter Filter, cursorReq *cursor.Request) (ListResult, error)

	// GetByID returns a single event by its ID.
	GetByID(ctx context.Context, eventID id.ID) (Event, error)

	// GetByTraceID returns all events sharing the same trace ID, ordered chronologically.
	GetByTraceID(ctx context.Context, traceID string) ([]Event, error)

	// GetStats returns aggregated counts by severity for the given time range.
	GetStats(ctx context.Context, filter StatsFilter) (Stats, error)
}
