//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	platformtelemetry "github.com/alex-bogatiuk/metapus/internal/platform/telemetry"
)

type traceCapturingOutboxHandler struct {
	traceID       trace.TraceID
	correlationID string
	causationID   string
}

func (h *traceCapturingOutboxHandler) Handle(ctx context.Context, _ *OutboxMessage) error {
	h.traceID = trace.SpanContextFromContext(ctx).TraceID()
	business := platformtelemetry.BusinessIDs(ctx, "")
	h.correlationID, h.causationID = business.CorrelationID, business.CausationID
	return nil
}

func TestRemediationOutboxTracePropagationAndDLQ(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

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
	ctx = platformtelemetry.WithBusinessContext(ctx, "request-123", "payment-456")
	txm := NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	aggregateID := id.New()
	event := domain.DomainEvent{AggregateType: "TestTrace", AggregateID: aggregateID,
		EventType: "trace.test", Payload: map[string]string{"id": aggregateID.String()}}
	require.NoError(t, txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return NewOutboxPublisher().Publish(txCtx, event)
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sys_outbox WHERE aggregate_type='TestTrace' AND aggregate_id=$1`, aggregateID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM sys_outbox_dlq WHERE aggregate_type='TestTrace' AND aggregate_id=$1`, aggregateID)
	})

	var msg OutboxMessage
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT id,created_at,traceparent,correlation_id,causation_id
		FROM sys_outbox WHERE aggregate_type='TestTrace' AND aggregate_id=$1
	`, aggregateID).Scan(&msg.ID, &msg.CreatedAt, &msg.Traceparent, &msg.CorrelationID, &msg.CausationID))
	require.Equal(t, "00-abababababababababababababababab-cdcdcdcdcdcdcdcd-01", msg.Traceparent)
	require.Equal(t, "request-123", msg.CorrelationID)
	require.Equal(t, "payment-456", msg.CausationID)

	handler := &traceCapturingOutboxHandler{}
	relay := NewOutboxRelay(pool, 1, handler).FilterAggregates([]string{"TestTrace"}, nil)
	processed, err := relay.ProcessBatch(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, traceID, handler.traceID)
	require.Equal(t, "request-123", handler.correlationID)
	require.Equal(t, "payment-456", handler.causationID)

	_, err = pool.Exec(ctx, `
		UPDATE sys_outbox SET status='failed',retry_count=5,last_error=$1
		WHERE id=$2 AND created_at=$3
	`, errors.New("test failed delivery").Error(), msg.ID, msg.CreatedAt)
	require.NoError(t, err)
	_, err = relay.MoveToDLQ(ctx)
	require.NoError(t, err)
	var movedTraceparent, movedCorrelation, movedCausation string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT traceparent,correlation_id,causation_id
		FROM sys_outbox_dlq WHERE id=$1
	`, msg.ID).Scan(&movedTraceparent, &movedCorrelation, &movedCausation))
	require.Equal(t, msg.Traceparent, movedTraceparent)
	require.Equal(t, msg.CorrelationID, movedCorrelation)
	require.Equal(t, msg.CausationID, movedCausation)
}
