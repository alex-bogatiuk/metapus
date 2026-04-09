package dto

import (
	"time"

	"metapus/internal/core/eventlog"
	"metapus/internal/core/id"
)

// EventLogResponse is the API response for a single event log entry.
type EventLogResponse struct {
	ID           string         `json:"id"`
	Category     string         `json:"category"`
	Severity     string         `json:"severity"`
	EventType    string         `json:"eventType"`
	Source       string         `json:"source"`
	SessionID    string         `json:"sessionId,omitempty"`
	UserID       string         `json:"userId,omitempty"`
	UserEmail    string         `json:"userEmail,omitempty"`
	ClientIP     string         `json:"clientIp,omitempty"`
	EntityType   string         `json:"entityType,omitempty"`
	EntityID     string         `json:"entityId,omitempty"`
	EntityNumber string         `json:"entityNumber,omitempty"`
	Message      string         `json:"message"`
	Details      map[string]any `json:"details,omitempty"`
	TraceID      string         `json:"traceId,omitempty"`
	RequestID    string         `json:"requestId,omitempty"`
	DurationMs   *int           `json:"durationMs,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
}

// FromEventLogEntry maps a domain Event to an API response.
func FromEventLogEntry(e eventlog.Event) EventLogResponse {
	r := EventLogResponse{
		ID:           e.ID.String(),
		Category:     string(e.Category),
		Severity:     string(e.Severity),
		EventType:    string(e.EventType),
		Source:       e.Source,
		SessionID:    e.SessionID,
		UserID:       e.UserID,
		UserEmail:    e.UserEmail,
		ClientIP:     e.ClientIP,
		EntityType:   e.EntityType,
		EntityNumber: e.EntityNumber,
		Message:      e.Message,
		Details:      e.Details,
		TraceID:      e.TraceID,
		RequestID:    e.RequestID,
		DurationMs:   e.DurationMs,
		CreatedAt:    e.CreatedAt,
	}
	if e.EntityID != nil && !id.IsNil(*e.EntityID) {
		r.EntityID = e.EntityID.String()
	}
	return r
}

// FromEventLogEntries maps a slice of domain Events to API responses.
func FromEventLogEntries(events []eventlog.Event) []EventLogResponse {
	items := make([]EventLogResponse, len(events))
	for i, e := range events {
		items[i] = FromEventLogEntry(e)
	}
	return items
}

// EventLogStatsResponse is the API response for event log statistics.
type EventLogStatsResponse struct {
	Total    int64 `json:"total"`
	Info     int64 `json:"info"`
	Warning  int64 `json:"warning"`
	Error    int64 `json:"error"`
	Critical int64 `json:"critical"`
}

// FromEventLogStats maps domain Stats to API response.
func FromEventLogStats(s eventlog.Stats) EventLogStatsResponse {
	return EventLogStatsResponse{
		Total:    s.Total,
		Info:     s.Info,
		Warning:  s.Warning,
		Error:    s.Error,
		Critical: s.Critical,
	}
}
