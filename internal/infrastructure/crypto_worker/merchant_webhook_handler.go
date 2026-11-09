package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/workerjob"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	platformtelemetry "metapus/internal/platform/telemetry"
)

// MerchantWebhookHandler delivers durable lifecycle events emitted by the
// crypto processor. Configuration and secrets are resolved only at delivery.
type MerchantWebhookHandler struct {
	dispatcher      merchantWebhookDispatcher
	deliveryRepo    crypto.WebhookDeliveryRepository
	endpointRepo    crypto.MerchantWebhookRepository
	secrets         crypto.WebhookSecretStore
	now             func() time.Time
	deliveryTimeout time.Duration
}

// CleanupRetiredSecrets removes versioned Vault keys only after the durable
// grace period that covers every delivery snapshot. Claim leases make it safe
// for horizontally scaled workers and a Vault 404 is idempotent.
func (h *MerchantWebhookHandler) CleanupRetiredSecrets(ctx context.Context, limit int) (int, error) {
	retirements, ok := h.endpointRepo.(crypto.WebhookSecretRetirementRepository)
	if !ok {
		return 0, nil
	}
	if h.secrets == nil {
		return 0, fmt.Errorf("merchant webhook Vault secret store is not configured")
	}
	now := h.now().UTC()
	records, err := retirements.ClaimDueSecretRetirements(ctx, now, limit)
	if err != nil {
		return 0, fmt.Errorf("claim retired webhook secrets: %w", err)
	}
	completed := 0
	var cleanupErrors []error
	for _, record := range records {
		if deleteErr := h.secrets.Delete(ctx, record.SecretRef); deleteErr != nil {
			delay := time.Minute * time.Duration(1<<min(record.AttemptCount, 8))
			if delay > 6*time.Hour {
				delay = 6 * time.Hour
			}
			if retryErr := retirements.RetrySecretRetirement(ctx, record.SecretRef, now.Add(delay), deleteErr.Error()); retryErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("retry retired webhook secret %s: %w", record.SecretRef, retryErr))
			} else {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("delete retired webhook secret %s: %w", record.SecretRef, deleteErr))
			}
			continue
		}
		if completeErr := retirements.CompleteSecretRetirement(ctx, record.SecretRef); completeErr != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("complete retired webhook secret %s: %w", record.SecretRef, completeErr))
			continue
		}
		completed++
	}
	return completed, errors.Join(cleanupErrors...)
}

type merchantWebhookDispatcher interface {
	Dispatch(context.Context, crypto.WebhookDeliveryRepository, crypto.WebhookDispatchRequest) (*crypto.WebhookDelivery, error)
}

func NewMerchantWebhookHandler(
	dispatcher merchantWebhookDispatcher,
	deliveryRepo crypto.WebhookDeliveryRepository,
	endpointRepo crypto.MerchantWebhookRepository,
	secrets crypto.WebhookSecretStore,
) *MerchantWebhookHandler {
	return &MerchantWebhookHandler{
		dispatcher: dispatcher, deliveryRepo: deliveryRepo,
		endpointRepo: endpointRepo, secrets: secrets, now: time.Now, deliveryTimeout: 45 * time.Second,
	}
}

func (h *MerchantWebhookHandler) Handle(ctx context.Context, msg *postgres.OutboxMessage) error {
	if msg.AggregateType != crypto.MerchantWebhookAggregateType {
		return fmt.Errorf("unsupported merchant webhook aggregate %q", msg.AggregateType)
	}
	var event crypto.LifecycleWebhookEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return fmt.Errorf("unmarshal lifecycle webhook: %w", err)
	}
	if event.AggregateID != msg.AggregateID || string(event.Event) != msg.EventType {
		return fmt.Errorf("lifecycle webhook envelope does not match outbox metadata")
	}
	if event.ID == "" {
		event.ID = "evt_" + id.Compact(msg.ID)
	}
	if h.endpointRepo == nil {
		return fmt.Errorf("merchant webhook endpoint repository is not configured")
	}
	return h.endpointRepo.PrepareAutomaticJobs(ctx, event)
}

var webhookAttemptOffsets = []time.Duration{
	0, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour,
	6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 48 * time.Hour, 72 * time.Hour,
}

// DeliverJob runs only after a fenced delivery claim. Outbox handling only
// prepares durable jobs and never waits for a merchant's HTTP endpoint.
func (h *MerchantWebhookHandler) DeliverJob(ctx context.Context, job crypto.WebhookDeliveryJob) error {
	if h.endpointRepo == nil {
		return fmt.Errorf("merchant webhook endpoint repository is unavailable")
	}
	var event crypto.LifecycleWebhookEvent
	if err := json.Unmarshal(job.EventPayload, &event); err != nil {
		return errors.Join(fmt.Errorf("decode delivery event: %w", err), h.endpointRepo.MarkJobRetry(ctx, job.ID, job.ClaimToken, job.AttemptCount+1, h.now().UTC(), true, err.Error()))
	}
	if event.ID != job.EventID || event.MerchantID != job.MerchantID {
		err := fmt.Errorf("delivery event does not match its job")
		return errors.Join(err, h.endpointRepo.MarkJobRetry(ctx, job.ID, job.ClaimToken, job.AttemptCount+1, h.now().UTC(), true, err.Error()))
	}
	now := h.now().UTC()
	attempt := job.AttemptCount + 1
	var delivery *crypto.WebhookDelivery
	var dispatchErr error
	deliveryCtx, cancel := context.WithTimeout(ctx, h.deliveryTimeout)
	defer cancel()
	err := workerjob.WithLease(deliveryCtx, 20*time.Second, func(renewCtx context.Context) error {
		return h.endpointRepo.RenewJob(renewCtx, job.ID, job.ClaimToken)
	}, func(workCtx context.Context) error {
		if h.secrets == nil || h.deliveryRepo == nil || h.dispatcher == nil {
			dispatchErr = fmt.Errorf("merchant webhook delivery dependencies are unavailable")
			return nil
		}
		recordWebhookAttemptStart(ctx, event.Event, attempt, job.Manual, event.OccurredAt, now)
		activeSecret, secretErr := h.secrets.Get(workCtx, job.SecretRef)
		previousSecret := ""
		if secretErr == nil && job.PreviousSecretRef != "" && job.PreviousSecretValidUntil != nil && now.Before(*job.PreviousSecretValidUntil) {
			previousSecret, secretErr = h.secrets.Get(workCtx, job.PreviousSecretRef)
		}
		if secretErr == nil {
			business := platformtelemetry.BusinessIDs(workCtx, event.ID)
			delivery, dispatchErr = h.dispatcher.Dispatch(workCtx, h.deliveryRepo, crypto.WebhookDispatchRequest{
				InvoiceID: event.InvoiceID, MerchantID: event.MerchantID,
				WebhookURL: job.SnapshotURL, ActiveSecret: activeSecret,
				PreviousSecret: previousSecret, EventID: event.ID,
				APIVersion: event.APIVersion, Event: event.Event, OccurredAt: event.OccurredAt,
				Data: event.Data, RawBody: event.DeliveryPayload, Attempt: attempt,
				Traceparent: platformtelemetry.Traceparent(workCtx), CorrelationID: business.CorrelationID,
				CausationID: business.CausationID,
			})
		} else {
			dispatchErr = fmt.Errorf("load webhook secret: %w", secretErr)
			body := append([]byte(nil), event.DeliveryPayload...)
			if len(body) == 0 {
				var marshalErr error
				body, marshalErr = json.MarshalDeterministic(crypto.WebhookPayload{
					ID: event.ID, Object: "event", APIVersion: event.APIVersion,
					Type: event.Event, CreatedAt: event.OccurredAt.UTC(),
					Data: crypto.WebhookEventData{Object: event.Data},
				})
				if marshalErr != nil {
					return fmt.Errorf("marshal failed webhook attempt: %w", marshalErr)
				}
			}
			errMessage := dispatchErr.Error()
			delivery = &crypto.WebhookDelivery{
				ID: id.New(), InvoiceID: event.InvoiceID, MerchantID: event.MerchantID,
				EventType: event.Event, WebhookURL: job.SnapshotURL,
				EventID: event.ID, DeliveryID: "del_" + id.Compact(id.New()),
				Attempt: attempt, ErrorMessage: &errMessage, ErrorClass: "secret_load_error",
				RequestBody: body, CreatedAt: now,
			}
			if err := h.deliveryRepo.Create(workCtx, delivery); err != nil {
				return fmt.Errorf("persist failed webhook attempt: %w", err)
			}
		}

		return nil
	})
	if errors.Is(err, workerjob.ErrLeaseLost) || ctx.Err() != nil {
		return err
	}
	dispatchErr = errors.Join(dispatchErr, err)
	if dispatchErr == nil {
		if err := h.endpointRepo.MarkJobSucceeded(ctx, job.ID, job.ClaimToken); err != nil {
			return fmt.Errorf("complete webhook delivery job: %w", err)
		}
		recordWebhookAttemptOutcome(ctx, event.Event, attempt, job.Manual, true, false)
		return nil
	}

	scheduleStartedAt := event.OccurredAt
	if job.Manual && !job.ScheduleStartedAt.IsZero() {
		scheduleStartedAt = job.ScheduleStartedAt
	}
	next, terminal := webhookJobRetryAt(scheduleStartedAt, job.ID, attempt, job.DeadlineAt)
	if delivery != nil && delivery.RetryAfter != nil && delivery.RetryAfter.After(next) && delivery.RetryAfter.Before(job.DeadlineAt) {
		next = *delivery.RetryAfter
	}
	if err := h.endpointRepo.MarkJobRetry(ctx, job.ID, job.ClaimToken, attempt, next, terminal, dispatchErr.Error()); err != nil {
		return fmt.Errorf("reschedule webhook delivery job: %w", err)
	}
	recordWebhookAttemptOutcome(ctx, event.Event, attempt, job.Manual, false, terminal)
	if delivery != nil {
		var visibleNext *time.Time
		if !terminal {
			visibleNext = &next
		}
		if err := h.deliveryRepo.SetNextRetry(ctx, delivery.ID, visibleNext); err != nil {
			return fmt.Errorf("record webhook next retry: %w", err)
		}
	}
	return nil
}

func webhookJobRetryAt(occurredAt time.Time, jobID id.ID, attempt int, deadline time.Time) (time.Time, bool) {
	if attempt >= len(webhookAttemptOffsets) {
		return deadline, true
	}
	target := webhookAttemptOffsets[attempt]
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(jobID.String()))
	_, _ = hasher.Write([]byte{byte(attempt)})
	unit := float64(hasher.Sum64()%20001)/10000 - 1 // [-1,+1]
	jittered := time.Duration(float64(target) * (1 + unit*0.10))
	next := occurredAt.Add(jittered)
	if next.After(deadline) {
		// The last planned attempt is capped at the 72-hour deadline rather
		// than silently dropped when positive jitter crosses it.
		return deadline, false
	}
	return next, false
}
