//go:build integration

package crypto_repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/telemetry"
)

func TestLifecycleWebhookOutboxDedupRepeatTransitionAndRollback(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}
	ctx := context.Background()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previousPropagator) })
	traceID, err := trace.TraceIDFromHex("abababababababababababababababab")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("cdcdcdcdcdcdcdcd")
	require.NoError(t, err)
	ctx = trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	ctx = telemetry.WithBusinessContext(ctx, "request-123", "payment-456")
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	aggregateID, merchantID := id.New(), id.New()
	errRollback := errors.New("rollback integration fixture")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		_, err := q.Exec(txCtx, `INSERT INTO cat_merchants(id,code,name,kyb_status) VALUES($1,$2,$3,'approved')`,
			merchantID, "integration-"+id.Compact(merchantID), "Lifecycle outbox integration")
		require.NoError(t, err)

		publisher := NewLifecycleWebhookOutbox()
		event := crypto.LifecycleWebhookEvent{
			Event: crypto.WebhookInvoicePaid, AggregateID: aggregateID,
			MerchantID: merchantID, ResourceVersion: 1, OccurredAt: time.Now().UTC(),
			Data: map[string]any{"invoiceId": aggregateID.String()},
		}
		require.NoError(t, publisher.PublishLifecycleWebhook(txCtx, event))
		require.NoError(t, publisher.PublishLifecycleWebhook(txCtx, event))
		var storedEnvelope []byte
		require.NoError(t, q.QueryRow(txCtx, `SELECT payload FROM sys_merchant_events WHERE merchant_id=$1 AND event_type=$2`,
			merchantID, crypto.WebhookInvoicePaid).Scan(&storedEnvelope))
		var storedEvent crypto.LifecycleWebhookEvent
		require.NoError(t, json.Unmarshal(storedEnvelope, &storedEvent))
		require.NotEmpty(t, storedEvent.DeliveryPayload)
		var publicPayload crypto.WebhookPayload
		require.NoError(t, json.Unmarshal(storedEvent.DeliveryPayload, &publicPayload))
		require.Equal(t, storedEvent.ID, publicPayload.ID)
		require.Equal(t, crypto.WebhookInvoicePaid, publicPayload.Type)

		var count int
		require.NoError(t, q.QueryRow(txCtx, `SELECT COUNT(*) FROM sys_outbox WHERE aggregate_type=$1 AND aggregate_id=$2 AND event_type=$3`,
			crypto.MerchantWebhookAggregateType, aggregateID, crypto.WebhookInvoicePaid).Scan(&count))
		require.Equal(t, 1, count)
		var eventTrace, outboxTrace, eventCorrelation, outboxCorrelation, outboxCausation string
		require.NoError(t, q.QueryRow(txCtx, `
			SELECT me.traceparent,o.traceparent,me.correlation_id,o.correlation_id,o.causation_id
			FROM sys_merchant_events me JOIN sys_outbox o ON o.id=me.id
			WHERE me.merchant_id=$1 AND me.event_type=$2
		`, merchantID, crypto.WebhookInvoicePaid).Scan(
			&eventTrace, &outboxTrace, &eventCorrelation, &outboxCorrelation, &outboxCausation))
		require.Equal(t, "00-abababababababababababababababab-cdcdcdcdcdcdcdcd-01", eventTrace)
		require.Equal(t, eventTrace, outboxTrace)
		require.Equal(t, "request-123", eventCorrelation)
		require.Equal(t, eventCorrelation, outboxCorrelation)
		require.NotEmpty(t, outboxCausation)

		// A later lawful transition of the same event type has a new resource
		// version and therefore a distinct causation key and immutable event ID.
		event.ResourceVersion = 2
		event.OccurredAt = event.OccurredAt.Add(time.Second)
		require.NoError(t, publisher.PublishLifecycleWebhook(txCtx, event))
		require.NoError(t, q.QueryRow(txCtx, `SELECT COUNT(*) FROM sys_merchant_events WHERE merchant_id=$1 AND event_type=$2`,
			merchantID, crypto.WebhookInvoicePaid).Scan(&count))
		require.Equal(t, 2, count)
		require.NoError(t, q.QueryRow(txCtx, `SELECT COUNT(*) FROM sys_outbox WHERE aggregate_type=$1 AND aggregate_id=$2 AND event_type=$3`,
			crypto.MerchantWebhookAggregateType, aggregateID, crypto.WebhookInvoicePaid).Scan(&count))
		require.Equal(t, 2, count)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)

	// Merchant event, dedup marker, and outbox message share the business
	// transaction and are all absent after rollback.
	var merchantEvents, outboxMessages int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM sys_merchant_events WHERE merchant_id=$1`, merchantID).Scan(&merchantEvents))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM sys_outbox WHERE aggregate_type=$1 AND aggregate_id=$2`,
		crypto.MerchantWebhookAggregateType, aggregateID).Scan(&outboxMessages))
	require.Zero(t, merchantEvents)
	require.Zero(t, outboxMessages)
}
