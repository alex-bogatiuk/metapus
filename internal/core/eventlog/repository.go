package eventlog

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain/cursor"

	"github.com/jackc/pgx/v5/pgxpool"
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

// DirectWriter records events bypassing TxManager, writing directly via pgxpool.Pool.
// Use in middleware that runs before TenantDB (Recovery, Logger) where TxManager is unavailable.
type DirectWriter interface {
	// WriteDirect records a single event using the provided pool (no TxManager needed).
	WriteDirect(ctx context.Context, pool *pgxpool.Pool, event Event) error
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
