package aml

import (
	"time"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
)

type JobStatus string

const (
	JobPendingSubmit     JobStatus = "pending_submit"
	JobSubmitting        JobStatus = "submitting"
	JobSubmissionUnknown JobStatus = "submission_unknown"
	JobAwaitingResult    JobStatus = "awaiting_result"
	JobPolling           JobStatus = "polling"
	JobSucceeded         JobStatus = "succeeded"
	JobNeedsAttention    JobStatus = "needs_attention"
)

type Job struct {
	ID                id.ID
	SubjectID         id.ID
	Generation        int64
	Provider          string
	ProviderAccountID id.ID
	Request           CheckRequest
	RequestDigest     string
	MappingVersion    string
	SubmitRequestID   string
	PollRequestID     string
	Ticket            *Ticket
	Status            JobStatus
	LeaseToken        id.ID
	LeaseExpiresAt    time.Time
	DeadlineAt        time.Time
}

func (j Job) ValidateTicket(ticket Ticket) error {
	if ticket.Provider != j.Provider || ticket.ProviderAccountID != j.ProviderAccountID || ticket.SubmitRequestID != j.SubmitRequestID ||
		ticket.MappingVersion != j.MappingVersion || ticket.Request != j.Request || ticket.RequestDigest != j.RequestDigest ||
		ticket.RequestDigest != ticket.Request.Digest(ticket.MappingVersion) || ticket.CheckHash == "" || ticket.SubmittedAt.IsZero() {
		return apperror.NewValidation("AML ticket does not match durable job identity")
	}
	return nil
}
