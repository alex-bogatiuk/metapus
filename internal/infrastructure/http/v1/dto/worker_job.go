package dto

import (
	"metapus/internal/core/workerjob"
)

// WorkerJobResponse is the API representation of a single worker job run.
type WorkerJobResponse struct {
	ID             string  `json:"id"`
	JobName        string  `json:"jobName"`
	JobCategory    string  `json:"jobCategory"`
	Status         string  `json:"status"`
	StartedAt      string  `json:"startedAt"`
	FinishedAt     *string `json:"finishedAt"`
	DurationMs     *int    `json:"durationMs"`
	ItemsProcessed *int    `json:"itemsProcessed"`
	ErrorMessage   *string `json:"errorMessage,omitempty"`
}

// WorkerJobStatsResponse is the API representation of KPI stats.
type WorkerJobStatsResponse struct {
	Total       int64 `json:"total"`
	Success     int64 `json:"success"`
	Error       int64 `json:"error"`
	AvgDuration int64 `json:"avgDuration"` // milliseconds
}

// WorkerJobListResponse is the paginated list response.
type WorkerJobListResponse struct {
	Items      []WorkerJobResponse `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
	HasMore    bool                `json:"hasMore"`
	TotalCount int64               `json:"totalCount"`
}

// MapWorkerJob converts a domain Job to its DTO.
func MapWorkerJob(j workerjob.Job) WorkerJobResponse {
	r := WorkerJobResponse{
		ID:             j.ID.String(),
		JobName:        j.JobName,
		JobCategory:    j.JobCategory,
		Status:         string(j.Status),
		StartedAt:      j.StartedAt.UTC().Format("2006-01-02T15:04:05Z"),
		DurationMs:     j.DurationMs,
		ItemsProcessed: j.ItemsProcessed,
		ErrorMessage:   j.ErrorMessage,
	}
	if j.FinishedAt != nil {
		s := j.FinishedAt.UTC().Format("2006-01-02T15:04:05Z")
		r.FinishedAt = &s
	}
	return r
}

// MapWorkerJobs converts a slice of domain Jobs to DTOs.
func MapWorkerJobs(jobs []workerjob.Job) []WorkerJobResponse {
	out := make([]WorkerJobResponse, len(jobs))
	for i, j := range jobs {
		out[i] = MapWorkerJob(j)
	}
	return out
}
