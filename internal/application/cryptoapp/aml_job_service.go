package cryptoapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"metapus/internal/domain/crypto/aml"
)

type AMLJobRepository interface {
	ClaimAMLJob(context.Context, time.Time, time.Duration) (aml.Job, bool, error)
	SaveAMLTicket(context.Context, aml.Job, aml.Ticket, time.Time) error
	SaveAMLReport(context.Context, aml.Job, aml.Report) error
	RescheduleAMLPoll(context.Context, aml.Job, time.Time) error
	MarkAMLAttention(context.Context, aml.Job, aml.JobStatus, string) error
}

type AMLProviderResolver interface {
	ResolveAMLProvider(context.Context, aml.Job) (aml.Provider, error)
}

// AMLJobService performs one leased HTTP operation per run. SQL locks are
// released by Claim before any provider I/O; submitting leases never turn
// back into a blind paid retry after a crash.
type AMLJobService struct {
	repo         AMLJobRepository
	providers    AMLProviderResolver
	lease        time.Duration
	pollInterval time.Duration
	now          func() time.Time
}

func NewAMLJobService(repo AMLJobRepository, providers AMLProviderResolver) *AMLJobService {
	if repo == nil || providers == nil {
		panic("cryptoapp.NewAMLJobService: repository and pinned provider resolver are required")
	}
	return &AMLJobService{repo: repo, providers: providers, lease: 30 * time.Second, pollInterval: 5 * time.Second, now: time.Now}
}

func (s *AMLJobService) RunOnce(ctx context.Context) (bool, error) {
	job, claimed, err := s.repo.ClaimAMLJob(ctx, s.now().UTC(), s.lease)
	if err != nil || !claimed {
		return claimed, err
	}
	if job.Request.Validate() != nil || job.MappingVersion == "" || job.RequestDigest != job.Request.Digest(job.MappingVersion) {
		return true, s.repo.MarkAMLAttention(ctx, job, aml.JobNeedsAttention, "invalid_request_binding")
	}
	provider, err := s.providers.ResolveAMLProvider(ctx, job)
	if err != nil {
		return true, s.repo.MarkAMLAttention(ctx, job, aml.JobNeedsAttention, "provider_unavailable")
	}
	if provider == nil || provider.Name() != job.Provider || provider.AccountID() != job.ProviderAccountID {
		return true, s.repo.MarkAMLAttention(ctx, job, aml.JobNeedsAttention, "provider_account_mismatch")
	}
	switch job.Status {
	case aml.JobSubmitting:
		ticket, err := provider.Submit(ctx, job.Request, job.SubmitRequestID)
		if err != nil {
			status := aml.JobNeedsAttention
			var failure aml.ProviderFailure
			if errors.As(err, &failure) && failure.OutcomeUnknown() {
				status = aml.JobSubmissionUnknown
			}
			return true, s.repo.MarkAMLAttention(ctx, job, status, "submit_failed")
		}
		if err := job.ValidateTicket(ticket); err != nil {
			return true, s.repo.MarkAMLAttention(ctx, job, aml.JobSubmissionUnknown, "submit_ticket_mismatch")
		}
		return true, s.repo.SaveAMLTicket(ctx, job, ticket, s.now().UTC().Add(s.pollInterval))
	case aml.JobPolling:
		if job.Ticket == nil || job.ValidateTicket(*job.Ticket) != nil || job.PollRequestID == "" {
			return true, s.repo.MarkAMLAttention(ctx, job, aml.JobNeedsAttention, "missing_or_invalid_ticket")
		}
		result, err := provider.GetResult(ctx, *job.Ticket, job.PollRequestID)
		if err != nil {
			var failure aml.ProviderFailure
			if errors.As(err, &failure) && failure.Temporary() {
				return true, s.repo.RescheduleAMLPoll(ctx, job, s.now().UTC().Add(s.pollInterval))
			}
			return true, s.repo.MarkAMLAttention(ctx, job, aml.JobNeedsAttention, "poll_failed")
		}
		switch result.State {
		case aml.CheckPending:
			return true, s.repo.RescheduleAMLPoll(ctx, job, s.now().UTC().Add(s.pollInterval))
		case aml.CheckSucceeded:
			if result.Report == nil {
				return true, s.repo.MarkAMLAttention(ctx, job, aml.JobNeedsAttention, "missing_report")
			}
			return true, s.repo.SaveAMLReport(ctx, job, result.Report.Clone())
		default:
			return true, s.repo.MarkAMLAttention(ctx, job, aml.JobNeedsAttention, "vendor_check_failed")
		}
	default:
		return true, fmt.Errorf("claim AML job returned non-executable state %q", job.Status)
	}
}
