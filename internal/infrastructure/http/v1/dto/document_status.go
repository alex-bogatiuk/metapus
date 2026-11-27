package dto

import "time"

// DocumentStatusChange is one row of a document's status history: the
// status the document was written in and the one it had before (empty for a
// new document), with their names, when, and who wrote it (empty for the
// system).
type DocumentStatusChange struct {
	Status              string    `json:"status"`
	StatusLabel         string    `json:"statusLabel"`
	PreviousStatus      string    `json:"previousStatus,omitempty"`
	PreviousStatusLabel string    `json:"previousStatusLabel,omitempty"`
	ChangedAt           time.Time `json:"changedAt"`
	ChangedBy           string    `json:"changedBy,omitempty"`
	ChangedByName       string    `json:"changedByName,omitempty"`
}

// DocumentStatusHistoryResponse is GET /document/<route>/:id/status-history:
// the document's status changes, oldest first.
type DocumentStatusHistoryResponse struct {
	Items []DocumentStatusChange `json:"items"`
}
