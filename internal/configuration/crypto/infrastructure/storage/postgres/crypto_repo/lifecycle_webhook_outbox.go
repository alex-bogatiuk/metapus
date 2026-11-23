package crypto_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/telemetry"
)

// LifecycleWebhookOutbox atomically and idempotently publishes merchant
// lifecycle events. It must be called inside the business transaction.
type LifecycleWebhookOutbox struct{}

func NewLifecycleWebhookOutbox() *LifecycleWebhookOutbox { return &LifecycleWebhookOutbox{} }

func (p *LifecycleWebhookOutbox) PublishLifecycleWebhook(ctx context.Context, event crypto.LifecycleWebhookEvent) error {
	if id.IsNil(event.AggregateID) || id.IsNil(event.MerchantID) {
		return fmt.Errorf("lifecycle webhook aggregate and merchant IDs are required")
	}
	if event.Event == "" {
		return fmt.Errorf("lifecycle webhook event type is required")
	}
	if event.ResourceVersion < 1 {
		return fmt.Errorf("lifecycle webhook resource version must be positive")
	}

	txm := postgres.MustGetTxManager(ctx)
	tx := txm.GetTx(ctx)
	if tx == nil {
		return fmt.Errorf("lifecycle webhook publish requires transaction context")
	}

	now := time.Now().UTC()
	messageID := id.New()
	if event.ID == "" {
		event.ID = "evt_" + id.Compact(messageID)
	}
	if event.APIVersion == "" {
		event.APIVersion = crypto.MerchantWebhookAPIVersion
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	if event.CausationKey == "" {
		event.CausationKey = fmt.Sprintf("%s:%s:%d", event.AggregateID, event.Event, event.ResourceVersion)
	}
	// The domain event carries aggregate facts only. Resolve stable merchant
	// catalog labels here, inside the same transaction, while constructing the
	// versioned public event contract.
	if event.InvoiceID != nil {
		var currency, network string
		if err := tx.QueryRow(ctx, `
			SELECT t.code,n.code
			FROM doc_crypto_invoices ci
			JOIN cat_tokens t ON t.id=ci.token_id
			JOIN cat_blockchain_networks n ON n.id=t.network_id
			WHERE ci.id=$1
		`, *event.InvoiceID).Scan(&currency, &network); err != nil {
			return fmt.Errorf("resolve merchant event asset snapshot: %w", err)
		}
		if event.Data == nil {
			event.Data = make(map[string]any)
		}
		event.Data["currency"] = currency
		event.Data["network"] = network
	}
	deliveryPayload, err := json.MarshalDeterministic(crypto.WebhookPayload{
		ID: event.ID, Object: "event", APIVersion: event.APIVersion,
		Type: event.Event, CreatedAt: event.OccurredAt.UTC(),
		Data: crypto.WebhookEventData{Object: event.Data},
	})
	if err != nil {
		return fmt.Errorf("marshal immutable merchant delivery payload: %w", err)
	}
	event.DeliveryPayload = deliveryPayload
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal lifecycle webhook: %w", err)
	}
	business := telemetry.BusinessIDs(ctx, event.CausationKey)
	traceparent := telemetry.Traceparent(ctx)
	var inserted bool
	if err := tx.QueryRow(ctx, `
		WITH claimed AS (
			INSERT INTO sys_merchant_events(
				id,event_id,merchant_id,aggregate_type,aggregate_id,invoice_id,
				event_type,api_version,resource_version,causation_key,payload,created_at,
				traceparent,correlation_id)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (merchant_id,causation_key) DO NOTHING
			RETURNING TRUE
		)
		SELECT COALESCE((SELECT TRUE FROM claimed),FALSE)
	`, messageID, event.ID, event.MerchantID, crypto.MerchantWebhookAggregateType,
		event.AggregateID, event.InvoiceID, event.Event, event.APIVersion,
		event.ResourceVersion, event.CausationKey, payload, event.OccurredAt,
		traceparent, business.CorrelationID).Scan(&inserted); err != nil {
		return fmt.Errorf("persist immutable merchant event: %w", err)
	}
	if !inserted {
		return nil
	}

	if err := postgres.EnqueueOutbox(ctx, postgres.OutboxRecord{
		ID: messageID, AggregateType: crypto.MerchantWebhookAggregateType, AggregateID: event.AggregateID,
		EventType: string(event.Event), Payload: payload, CreatedAt: now,
		Traceparent: traceparent, CorrelationID: business.CorrelationID, CausationID: event.CausationKey,
	}); err != nil {
		return fmt.Errorf("insert lifecycle webhook outbox message: %w", err)
	}
	return nil
}

var _ crypto.LifecycleWebhookPublisher = (*LifecycleWebhookOutbox)(nil)
