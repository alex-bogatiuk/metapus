// Package workerjob provides domain types and repository interface for background
// worker task execution observability.
package workerjob

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// Status of a worker job execution.
type Status string

const (
	StatusRunning Status = "running"
	StatusSuccess Status = "success"
	StatusError   Status = "error"
	StatusSkipped Status = "skipped"
)

// Job is an immutable record of a single background task execution.
type Job struct {
	ID             id.ID
	JobName        string
	JobCategory    string
	Status         Status
	StartedAt      time.Time
	FinishedAt     *time.Time
	DurationMs     *int
	ItemsProcessed *int
	ErrorMessage   *string
	Metadata       map[string]any
}

// Filter for listing job runs.
type Filter struct {
	JobName     string
	JobCategory string
	Status      string
	DateFrom    *time.Time
	DateTo      *time.Time
	// Cursor pagination
	After  string
	Before string
	Limit  int
}

// ListResult contains cursor-paginated job results.
type ListResult struct {
	Items      []Job
	NextCursor string
	PrevCursor string
	HasMore    bool
	HasPrev    bool
	TotalCount int64
}

// Stats aggregated counts for the last 24h (for KPI cards).
type Stats struct {
	Total       int64
	Success     int64
	Error       int64
	AvgDuration int64 // milliseconds
}

// Repository persists and retrieves worker job records.
type Repository interface {
	// Insert creates a new job run (status=running). Best-effort: callers must not fail on error.
	Insert(ctx context.Context, job *Job) error

	// Update finalises a running job (status=success|error|skipped, duration, items, error).
	Update(ctx context.Context, job *Job) error

	// List returns cursor-paginated job runs matching the filter.
	List(ctx context.Context, f Filter) (ListResult, error)

	// GetStats returns aggregated KPI counts for the given time window.
	GetStats(ctx context.Context, dateFrom, dateTo time.Time) (Stats, error)
}
