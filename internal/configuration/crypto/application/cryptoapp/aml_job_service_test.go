package cryptoapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/id"
)

type amlJobStoreStub struct {
	job       aml.Job
	claimed   bool
	status    aml.JobStatus
	reason    string
	ticket    *aml.Ticket
	report    *aml.Report
	pollAgain bool
}

func (r *amlJobStoreStub) ClaimAMLJob(context.Context, time.Time, time.Duration) (aml.Job, bool, error) {
	if r.claimed {
		return aml.Job{}, false, nil
	}
	r.claimed = true
	return r.job, true, nil
}
func (r *amlJobStoreStub) SaveAMLTicket(_ context.Context, _ aml.Job, ticket aml.Ticket, _ time.Time) error {
	r.ticket = &ticket
	return nil
}
func (r *amlJobStoreStub) SaveAMLReport(_ context.Context, _ aml.Job, report aml.Report) error {
	r.report = &report
	return nil
}
func (r *amlJobStoreStub) RescheduleAMLPoll(context.Context, aml.Job, time.Time) error {
	r.pollAgain = true
	return nil
}
func (r *amlJobStoreStub) MarkAMLAttention(_ context.Context, _ aml.Job, status aml.JobStatus, reason string) error {
	r.status, r.reason = status, reason
	return nil
}

type amlProviderStub struct {
	accountID     id.ID
	submits       int
	polls         int
	submitID      string
	pollID        string
	ticket        aml.Ticket
	submitFailure error
	pollFailure   error
	result        aml.CheckResult
}

func (p *amlProviderStub) Name() string     { return "getblock_net" }
func (p *amlProviderStub) AccountID() id.ID { return p.accountID }
func (p *amlProviderStub) Submit(_ context.Context, _ aml.CheckRequest, requestID string) (aml.Ticket, error) {
	p.submits++
	p.submitID = requestID
	return p.ticket, p.submitFailure
}
func (p *amlProviderStub) GetResult(_ context.Context, _ aml.Ticket, requestID string) (aml.CheckResult, error) {
	p.polls++
	p.pollID = requestID
	return p.result, p.pollFailure
}

type amlResolverStub struct{ provider *amlProviderStub }

func (r amlResolverStub) ResolveAMLProvider(context.Context, aml.Job) (aml.Provider, error) {
	return r.provider, nil
}

type amlFailureStub struct{ unknown, temporary bool }

func (amlFailureStub) Error() string          { return "synthetic upstream failure" }
func (e amlFailureStub) OutcomeUnknown() bool { return e.unknown }
func (e amlFailureStub) Temporary() bool      { return e.temporary }

func amlServiceFixture(status aml.JobStatus) (*amlJobStoreStub, *amlProviderStub) {
	request := aml.CheckRequest{MerchantID: id.New(), NetworkID: id.New(), TokenID: id.New(), Kind: aml.AddressSubject, Address: "test-address"}
	account := id.New()
	ticket := aml.Ticket{Provider: "getblock_net", ProviderAccountID: account, CheckHash: "test-hash", SubmitRequestID: "persisted-submit-id",
		Request: request, MappingVersion: "test-v1", RequestDigest: request.Digest("test-v1"), SubmittedAt: time.Now().UTC()}
	job := aml.Job{ID: id.New(), SubjectID: id.New(), Generation: 1, Provider: "getblock_net", ProviderAccountID: account,
		Request: request, MappingVersion: "test-v1", RequestDigest: ticket.RequestDigest, SubmitRequestID: ticket.SubmitRequestID,
		PollRequestID: "persisted-poll-id", Status: status, Ticket: &ticket, LeaseToken: id.New(), LeaseExpiresAt: time.Now().Add(time.Minute)}
	return &amlJobStoreStub{job: job}, &amlProviderStub{accountID: account, ticket: ticket}
}

func TestAMLServiceSubmitDoesNotPollInSameLease(t *testing.T) {
	repo, provider := amlServiceFixture(aml.JobSubmitting)
	service := NewAMLJobService(repo, amlResolverStub{provider})
	handled, err := service.RunOnce(t.Context())
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, 1, provider.submits)
	require.Zero(t, provider.polls)
	require.Equal(t, repo.job.SubmitRequestID, provider.submitID)
	require.NotNil(t, repo.ticket)
}

func TestAMLServiceLostAckStopsAtSubmissionUnknown(t *testing.T) {
	repo, provider := amlServiceFixture(aml.JobSubmitting)
	provider.submitFailure = amlFailureStub{unknown: true, temporary: true}
	service := NewAMLJobService(repo, amlResolverStub{provider})
	_, err := service.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, aml.JobSubmissionUnknown, repo.status)
	require.Nil(t, repo.ticket)
	_, err = service.RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, provider.submits)
}

func TestAMLServiceRejectsMismatchedProviderBeforePaidSubmit(t *testing.T) {
	repo, provider := amlServiceFixture(aml.JobSubmitting)
	provider.accountID = id.New()
	_, err := NewAMLJobService(repo, amlResolverStub{provider}).RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, aml.JobNeedsAttention, repo.status)
	require.Equal(t, "provider_account_mismatch", repo.reason)
	require.Zero(t, provider.submits)
}

func TestAMLServiceRejectsInvalidRequestDigestBeforePaidSubmit(t *testing.T) {
	repo, provider := amlServiceFixture(aml.JobSubmitting)
	repo.job.Request.MerchantID = id.New()
	_, err := NewAMLJobService(repo, amlResolverStub{provider}).RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, "invalid_request_binding", repo.reason)
	require.Zero(t, provider.submits)
}

func TestAMLServicePendingAndTemporaryPollNeverSubmitAgain(t *testing.T) {
	for _, temporaryFailure := range []bool{false, true} {
		repo, provider := amlServiceFixture(aml.JobPolling)
		provider.result = aml.CheckResult{State: aml.CheckPending}
		if temporaryFailure {
			provider.pollFailure = amlFailureStub{temporary: true}
		}
		_, err := NewAMLJobService(repo, amlResolverStub{provider}).RunOnce(t.Context())
		require.NoError(t, err)
		require.True(t, repo.pollAgain)
		require.Equal(t, repo.job.PollRequestID, provider.pollID)
		require.Zero(t, provider.submits)
		require.Nil(t, repo.report)
	}
}

func TestAMLServiceSuccessfulStatusWithoutReportNeedsAttention(t *testing.T) {
	repo, provider := amlServiceFixture(aml.JobPolling)
	provider.result = aml.CheckResult{State: aml.CheckSucceeded}
	_, err := NewAMLJobService(repo, amlResolverStub{provider}).RunOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, "missing_report", repo.reason)
	require.Nil(t, repo.report)
}
