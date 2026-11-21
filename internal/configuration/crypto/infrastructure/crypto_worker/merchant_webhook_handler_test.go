package crypto_worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/storage/postgres"
)

type captureWebhookDispatcher struct {
	requests []crypto.WebhookDispatchRequest
}

func (d *captureWebhookDispatcher) Dispatch(_ context.Context, _ crypto.WebhookDeliveryRepository, req crypto.WebhookDispatchRequest) (*crypto.WebhookDelivery, error) {
	d.requests = append(d.requests, req)
	return &crypto.WebhookDelivery{}, nil
}

type noopWebhookDeliveryRepo struct{}

func (noopWebhookDeliveryRepo) Create(context.Context, *crypto.WebhookDelivery) error { return nil }
func (noopWebhookDeliveryRepo) SetNextRetry(context.Context, id.ID, *time.Time) error { return nil }
func (noopWebhookDeliveryRepo) ListByInvoice(context.Context, id.ID, int, int) ([]crypto.WebhookDelivery, int, error) {
	return nil, 0, nil
}
func (noopWebhookDeliveryRepo) ListByMerchant(context.Context, id.ID, int, int) ([]crypto.WebhookDelivery, int, error) {
	return nil, 0, nil
}

type captureWebhookDeliveryRepo struct {
	created []*crypto.WebhookDelivery
	retries map[id.ID]*time.Time
}

func (r *captureWebhookDeliveryRepo) Create(_ context.Context, delivery *crypto.WebhookDelivery) error {
	r.created = append(r.created, delivery)
	return nil
}
func (r *captureWebhookDeliveryRepo) SetNextRetry(_ context.Context, deliveryID id.ID, next *time.Time) error {
	if r.retries == nil {
		r.retries = make(map[id.ID]*time.Time)
	}
	r.retries[deliveryID] = next
	return nil
}
func (*captureWebhookDeliveryRepo) ListByInvoice(context.Context, id.ID, int, int) ([]crypto.WebhookDelivery, int, error) {
	return nil, 0, nil
}
func (*captureWebhookDeliveryRepo) ListByMerchant(context.Context, id.ID, int, int) ([]crypto.WebhookDelivery, int, error) {
	return nil, 0, nil
}

type dueWebhookJobRepo struct {
	job             crypto.WebhookDeliveryJob
	retryAttempt    int
	retryNext       time.Time
	retryTerminal   bool
	retryLastError  string
	preparedEventID string
}

type retirementWebhookJobRepo struct {
	dueWebhookJobRepo
	records   []crypto.WebhookSecretRetirement
	completed []string
	retried   []string
	retryAt   time.Time
}

func (r *retirementWebhookJobRepo) ClaimDueSecretRetirements(context.Context, time.Time, int) ([]crypto.WebhookSecretRetirement, error) {
	return r.records, nil
}
func (r *retirementWebhookJobRepo) CompleteSecretRetirement(_ context.Context, secretRef string) error {
	r.completed = append(r.completed, secretRef)
	return nil
}
func (r *retirementWebhookJobRepo) RetrySecretRetirement(_ context.Context, secretRef string, next time.Time, _ string) error {
	r.retried = append(r.retried, secretRef)
	r.retryAt = next
	return nil
}

func (*dueWebhookJobRepo) CreateEndpoint(context.Context, *crypto.MerchantWebhookEndpoint) error {
	return nil
}
func (*dueWebhookJobRepo) GetEndpoint(context.Context, id.ID, id.ID) (*crypto.MerchantWebhookEndpoint, error) {
	return nil, nil
}
func (*dueWebhookJobRepo) ListEndpoints(context.Context, id.ID) ([]crypto.MerchantWebhookEndpoint, error) {
	return nil, nil
}
func (*dueWebhookJobRepo) UpdateEndpoint(context.Context, *crypto.MerchantWebhookEndpoint) error {
	return nil
}
func (*dueWebhookJobRepo) DeleteEndpoint(context.Context, id.ID, id.ID) error { return nil }
func (*dueWebhookJobRepo) HasDefaultEndpoint(context.Context, id.ID) (bool, error) {
	return true, nil
}
func (*dueWebhookJobRepo) RotateEndpointSecret(context.Context, id.ID, id.ID, string, time.Time) (*crypto.MerchantWebhookEndpoint, error) {
	return nil, nil
}
func (*dueWebhookJobRepo) ListEvents(context.Context, id.ID, int, int) ([]crypto.MerchantEventRecord, int, error) {
	return nil, 0, nil
}
func (*dueWebhookJobRepo) GetEvent(context.Context, id.ID, string) (*crypto.MerchantEventRecord, error) {
	return nil, nil
}
func (*dueWebhookJobRepo) ListEventDeliveries(context.Context, id.ID, string) ([]crypto.WebhookDelivery, error) {
	return nil, nil
}
func (r *dueWebhookJobRepo) PrepareAutomaticJobs(_ context.Context, event crypto.LifecycleWebhookEvent) error {
	r.preparedEventID = event.ID
	return nil
}
func (r *dueWebhookJobRepo) ListDueJobs(context.Context, string, time.Time) ([]crypto.WebhookDeliveryJob, bool, error) {
	return []crypto.WebhookDeliveryJob{r.job}, true, nil
}
func (*dueWebhookJobRepo) MarkJobSucceeded(context.Context, id.ID, id.ID) error { return nil }
func (r *dueWebhookJobRepo) MarkJobRetry(_ context.Context, _ id.ID, _ id.ID, attempt int, next time.Time, terminal bool, lastError string) error {
	r.retryAttempt, r.retryNext, r.retryTerminal, r.retryLastError = attempt, next, terminal, lastError
	return nil
}
func (*dueWebhookJobRepo) CreateManualJob(context.Context, id.ID, string, id.ID, time.Time) error {
	return nil
}

type failingWebhookSecretStore struct{ err error }

func (s failingWebhookSecretStore) Put(context.Context, string, string) error { return s.err }
func (s failingWebhookSecretStore) Get(context.Context, string) (string, error) {
	return "", s.err
}
func (s failingWebhookSecretStore) Delete(context.Context, string) error { return s.err }

type staticWebhookSecretStore struct{ secret string }

func (s staticWebhookSecretStore) Put(context.Context, string, string) error { return nil }
func (s staticWebhookSecretStore) Get(context.Context, string) (string, error) {
	return s.secret, nil
}
func (s staticWebhookSecretStore) Delete(context.Context, string) error { return nil }

func TestMerchantWebhookRetriesUseStableEventIDAndImmutableEventTimestamp(t *testing.T) {
	dispatcher := &captureWebhookDispatcher{}
	messageID, aggregateID, merchantID := id.New(), id.New(), id.New()
	occurredAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	jobRepo := &dueWebhookJobRepo{job: crypto.WebhookDeliveryJob{
		ID: id.New(), EventID: "evt_test", MerchantID: merchantID,
		SnapshotURL: "https://merchant.example/webhooks", SecretRef: "vault://active",
		DeadlineAt: occurredAt.Add(72 * time.Hour),
	}}
	handler := NewMerchantWebhookHandler(dispatcher, noopWebhookDeliveryRepo{}, jobRepo, staticWebhookSecretStore{secret: "secret"})
	handler.now = func() time.Time { return occurredAt.Add(time.Minute) }
	payload, err := json.Marshal(crypto.LifecycleWebhookEvent{
		ID: "evt_test", Event: crypto.WebhookInvoiceConfirmed, AggregateID: aggregateID,
		MerchantID: merchantID, OccurredAt: occurredAt, Data: map[string]any{"invoiceId": aggregateID.String()},
	})
	require.NoError(t, err)

	msg := &postgres.OutboxMessage{
		ID: messageID, AggregateType: crypto.MerchantWebhookAggregateType,
		AggregateID: aggregateID, EventType: string(crypto.WebhookInvoiceConfirmed), Payload: payload,
	}
	require.NoError(t, deliverWebhookForTest(handler, context.Background(), msg))
	jobRepo.job.AttemptCount = 2
	require.NoError(t, deliverWebhookForTest(handler, context.Background(), msg))

	require.Len(t, dispatcher.requests, 2)
	require.Equal(t, "evt_test", dispatcher.requests[0].EventID)
	require.Equal(t, dispatcher.requests[0].EventID, dispatcher.requests[1].EventID)
	require.Equal(t, occurredAt, dispatcher.requests[0].OccurredAt)
	require.Equal(t, dispatcher.requests[0].OccurredAt, dispatcher.requests[1].OccurredAt)
	require.Equal(t, 1, dispatcher.requests[0].Attempt)
	require.Equal(t, 3, dispatcher.requests[1].Attempt)
}

func TestMerchantWebhookOutboxPreparationDoesNotRequireDeliveryDependencies(t *testing.T) {
	jobRepo := &dueWebhookJobRepo{}
	dispatcher := &captureWebhookDispatcher{}
	handler := NewMerchantWebhookHandler(dispatcher, nil, jobRepo, nil)
	event := crypto.LifecycleWebhookEvent{
		ID: "evt_prepare_only", MerchantID: id.New(), AggregateID: id.New(),
		Event: crypto.WebhookInvoicePaid, OccurredAt: time.Now().UTC(),
	}
	payload, err := json.Marshal(event)
	require.NoError(t, err)
	require.NoError(t, handler.Handle(t.Context(), &postgres.OutboxMessage{ID: id.New(), AggregateType: crypto.MerchantWebhookAggregateType, AggregateID: event.AggregateID, EventType: string(event.Event), Payload: payload}))
	require.Equal(t, event.ID, jobRepo.preparedEventID)
	require.Empty(t, dispatcher.requests)
}

type blockedWebhookSecretStore struct{ staticWebhookSecretStore }

func (blockedWebhookSecretStore) Get(ctx context.Context, _ string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestWebhookDeliveryTimeoutSchedulesRetryAndReleasesClaim(t *testing.T) {
	now, merchantID := time.Now().UTC(), id.New()
	event := crypto.LifecycleWebhookEvent{ID: "evt_timeout", MerchantID: merchantID, Event: crypto.WebhookInvoicePaid, OccurredAt: now}
	payload, err := json.Marshal(event)
	require.NoError(t, err)
	repo := &dueWebhookJobRepo{job: crypto.WebhookDeliveryJob{ID: id.New(), EventID: event.ID, MerchantID: merchantID, EventPayload: payload, DeadlineAt: now.Add(72 * time.Hour)}}
	handler := NewMerchantWebhookHandler(&captureWebhookDispatcher{}, &captureWebhookDeliveryRepo{}, repo, blockedWebhookSecretStore{})
	handler.deliveryTimeout = time.Millisecond
	require.NoError(t, handler.DeliverJob(t.Context(), repo.job))
	require.Equal(t, 1, repo.retryAttempt)
	require.False(t, repo.retryTerminal)
	require.Contains(t, repo.retryLastError, "context deadline exceeded")
}

func TestWebhookCorruptPayloadFailsInsteadOfReclaimingForever(t *testing.T) {
	repo := &dueWebhookJobRepo{}
	handler := NewMerchantWebhookHandler(nil, nil, repo, nil)
	require.Error(t, handler.DeliverJob(t.Context(), crypto.WebhookDeliveryJob{ID: id.New(), EventPayload: []byte("invalid")}))
	require.True(t, repo.retryTerminal)
}

func TestWebhookJobRetryScheduleCoversAllTenAttemptsThroughDeadline(t *testing.T) {
	occurredAt := time.Date(2026, 8, 26, 6, 0, 0, 0, time.UTC)
	deadline := occurredAt.Add(72 * time.Hour)
	jobID := id.New()

	for attempt := 1; attempt < len(webhookAttemptOffsets); attempt++ {
		next, terminal := webhookJobRetryAt(occurredAt, jobID, attempt, deadline)
		if terminal {
			t.Fatalf("attempt %d became terminal before the final scheduled delivery", attempt)
		}
		base := webhookAttemptOffsets[attempt]
		lower := occurredAt.Add(time.Duration(float64(base) * 0.90))
		upper := occurredAt.Add(time.Duration(float64(base) * 1.10))
		if next.Before(lower) || next.After(upper) || next.After(deadline) {
			t.Fatalf("attempt %d next=%s outside jitter/deadline bounds [%s,%s]", attempt, next, lower, upper)
		}
	}

	next, terminal := webhookJobRetryAt(occurredAt, jobID, len(webhookAttemptOffsets), deadline)
	if !terminal || !next.Equal(deadline) {
		t.Fatalf("failure after final attempt = (%s,%t), want deadline and terminal", next, terminal)
	}
}

func TestManualRedeliveryRetryScheduleStartsAtManualJobCreation(t *testing.T) {
	manualStart := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	eventTime := manualStart.Add(-20 * 24 * time.Hour)
	merchantID, aggregateID := id.New(), id.New()
	job := crypto.WebhookDeliveryJob{
		ID: id.New(), EventID: "evt_old_manual", MerchantID: merchantID,
		SnapshotURL: "https://merchant.example/webhooks", SecretRef: "vault://missing",
		DeadlineAt: manualStart.Add(72 * time.Hour), Manual: true, ScheduleStartedAt: manualStart,
	}
	jobRepo := &dueWebhookJobRepo{job: job}
	handler := NewMerchantWebhookHandler(
		&captureWebhookDispatcher{}, &captureWebhookDeliveryRepo{}, jobRepo,
		failingWebhookSecretStore{err: errors.New("vault unavailable")},
	)
	handler.now = func() time.Time { return manualStart }
	payload, err := json.Marshal(crypto.LifecycleWebhookEvent{
		ID: "evt_old_manual", Event: crypto.WebhookInvoiceCompleted,
		AggregateID: aggregateID, MerchantID: merchantID, OccurredAt: eventTime,
	})
	require.NoError(t, err)

	require.NoError(t, deliverWebhookForTest(handler, context.Background(), &postgres.OutboxMessage{
		ID: id.New(), AggregateType: crypto.MerchantWebhookAggregateType,
		AggregateID: aggregateID, EventType: string(crypto.WebhookInvoiceCompleted), Payload: payload,
	}))
	wantNext, terminal := webhookJobRetryAt(manualStart, job.ID, 1, job.DeadlineAt)
	require.False(t, terminal)
	require.Equal(t, wantNext, jobRepo.retryNext)
	require.True(t, jobRepo.retryNext.After(manualStart), "manual retry must not collapse into the past")
}

func TestEndpointJobPersistsSecretLoadFailureForDebugger(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	eventTime := now.Add(-time.Minute)
	merchantID, aggregateID := id.New(), id.New()
	jobRepo := &dueWebhookJobRepo{job: crypto.WebhookDeliveryJob{
		ID: id.New(), EventID: "evt_secret_failure", MerchantID: merchantID,
		SnapshotURL: "https://merchant.example/webhooks", SecretRef: "vault://missing",
		DeadlineAt: eventTime.Add(72 * time.Hour),
	}}
	deliveryRepo := &captureWebhookDeliveryRepo{}
	dispatcher := &captureWebhookDispatcher{}
	handler := NewMerchantWebhookHandler(dispatcher, deliveryRepo, jobRepo, failingWebhookSecretStore{err: errors.New("vault unavailable")})
	handler.now = func() time.Time { return now }
	event := crypto.LifecycleWebhookEvent{
		ID: "evt_secret_failure", APIVersion: crypto.MerchantWebhookAPIVersion,
		Event: crypto.WebhookInvoiceCompleted, AggregateID: aggregateID, MerchantID: merchantID,
		OccurredAt: eventTime, Data: map[string]any{"id": aggregateID.String(), "status": "completed"},
	}
	payload, err := json.Marshal(event)
	require.NoError(t, err)

	err = deliverWebhookForTest(handler, context.Background(), &postgres.OutboxMessage{
		ID: id.New(), AggregateType: crypto.MerchantWebhookAggregateType,
		AggregateID: aggregateID, EventType: string(event.Event), Payload: payload,
	})
	require.NoError(t, err)
	require.Empty(t, dispatcher.requests)
	require.Len(t, deliveryRepo.created, 1)
	delivery := deliveryRepo.created[0]
	require.Equal(t, event.ID, delivery.EventID)
	require.Equal(t, "secret_load_error", delivery.ErrorClass)
	require.Contains(t, *delivery.ErrorMessage, "vault unavailable")
	require.NotEmpty(t, delivery.RequestBody)
	require.Equal(t, 1, jobRepo.retryAttempt)
	require.False(t, jobRepo.retryTerminal)
	require.Contains(t, jobRepo.retryLastError, "load webhook secret")
	require.Contains(t, deliveryRepo.retries, delivery.ID)
	require.NotNil(t, deliveryRepo.retries[delivery.ID])
}

func TestRetiredWebhookSecretsAreDeletedAndCompleted(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	repo := &retirementWebhookJobRepo{records: []crypto.WebhookSecretRetirement{{SecretRef: "merchant-webhooks/m/e/v1"}}}
	handler := NewMerchantWebhookHandler(&captureWebhookDispatcher{}, &captureWebhookDeliveryRepo{}, repo, staticWebhookSecretStore{secret: "secret"})
	handler.now = func() time.Time { return now }

	completed, err := handler.CleanupRetiredSecrets(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	require.Equal(t, []string{"merchant-webhooks/m/e/v1"}, repo.completed)
	require.Empty(t, repo.retried)
}

func TestRetiredWebhookSecretFailureIsRescheduled(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	repo := &retirementWebhookJobRepo{records: []crypto.WebhookSecretRetirement{{SecretRef: "merchant-webhooks/m/e/v1", AttemptCount: 2}}}
	handler := NewMerchantWebhookHandler(&captureWebhookDispatcher{}, &captureWebhookDeliveryRepo{}, repo, failingWebhookSecretStore{err: errors.New("vault unavailable")})
	handler.now = func() time.Time { return now }

	completed, err := handler.CleanupRetiredSecrets(context.Background(), 100)
	require.ErrorContains(t, err, "vault unavailable")
	require.Zero(t, completed)
	require.Equal(t, []string{"merchant-webhooks/m/e/v1"}, repo.retried)
	require.Equal(t, now.Add(4*time.Minute), repo.retryAt)
}

func (*dueWebhookJobRepo) RenewJob(context.Context, id.ID, id.ID) error { return nil }
func (r *dueWebhookJobRepo) ClaimDueJobs(context.Context, time.Time, int) ([]crypto.WebhookDeliveryJob, error) {
	return []crypto.WebhookDeliveryJob{r.job}, nil
}

func deliverWebhookForTest(handler *MerchantWebhookHandler, ctx context.Context, msg *postgres.OutboxMessage) error {
	if err := handler.Handle(ctx, msg); err != nil {
		return err
	}
	var event crypto.LifecycleWebhookEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return err
	}
	if event.ID == "" {
		event.ID = "evt_" + id.Compact(msg.ID)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	jobs, _, err := handler.endpointRepo.ListDueJobs(ctx, event.ID, handler.now())
	if err != nil {
		return err
	}
	for _, job := range jobs {
		job.EventID = event.ID
		job.MerchantID = event.MerchantID
		job.EventPayload = payload
		if err := handler.DeliverJob(ctx, job); err != nil {
			return err
		}
	}
	return nil
}
