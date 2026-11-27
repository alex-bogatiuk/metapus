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

// SequenceStateResponse is GET /document/<route>/:id/sequence of a document
// that closes a period: the sequence it restores, the period [periodFrom,
// periodTo) it closes, the moment up to which the sequence is restored
// (absent when no closing was posted yet), whether the posted document is
// outdated (documents before its period's end changed after it), and
// whether an earlier period is to be closed again before it can be posted.
type SequenceStateResponse struct {
	Sequence      string     `json:"sequence"`
	PeriodFrom    time.Time  `json:"periodFrom"`
	PeriodTo      time.Time  `json:"periodTo"`
	Boundary      *time.Time `json:"boundary,omitempty"`
	Outdated      bool       `json:"outdated"`
	EarlierBroken bool       `json:"earlierBroken"`
}
