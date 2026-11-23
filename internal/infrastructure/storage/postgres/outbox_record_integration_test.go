//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

func TestEnqueueOutboxKeepsTheCallersRecord(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(context.Background(), txm)
	aggregateType := "TestRecord" + id.Compact(id.New())[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sys_outbox WHERE aggregate_type=$1`, aggregateType)
	})

	fixed := OutboxRecord{
		ID: id.New(), AggregateType: aggregateType, AggregateID: id.New(), EventType: "record.fixed",
		Payload: []byte(`{"id":"evt_1"}`), CreatedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
		Traceparent: "00-abababababababababababababababab-cdcdcdcdcdcdcdcd-01", CorrelationID: "corr", CausationID: "cause",
	}
	fresh := OutboxRecord{AggregateType: aggregateType, AggregateID: id.New(), EventType: "record.fresh", Payload: []byte(`{"id":"evt_2"}`)}
	unkeyed := OutboxRecord{AggregateType: aggregateType, AggregateID: id.New(), EventType: "record.unkeyed", Payload: []byte(`{"other":1}`)}
	require.NoError(t, txm.RunInTransaction(ctx, func(ctx context.Context) error {
		return EnqueueOutbox(ctx, fixed, fresh, unkeyed)
	}))
	require.NoError(t, EnqueueOutbox(ctx))

	var stored OutboxRecord
	var status OutboxStatus
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT aggregate_id,event_type,payload,created_at,traceparent,correlation_id,causation_id,status
		FROM sys_outbox WHERE id=$1`, fixed.ID).Scan(&stored.AggregateID, &stored.EventType, &stored.Payload,
		&stored.CreatedAt, &stored.Traceparent, &stored.CorrelationID, &stored.CausationID, &status))
	require.Equal(t, OutboxStatusPending, status)
	require.Equal(t, fixed.AggregateID, stored.AggregateID)
	require.JSONEq(t, string(fixed.Payload), string(stored.Payload))
	require.True(t, fixed.CreatedAt.Equal(stored.CreatedAt))
	require.Equal(t, fixed.Traceparent, stored.Traceparent)
	require.Equal(t, "corr", stored.CorrelationID)
	require.Equal(t, "cause", stored.CausationID)

	var freshID id.ID
	var freshAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT id,created_at FROM sys_outbox WHERE aggregate_type=$1 AND event_type='record.fresh'`, aggregateType).Scan(&freshID, &freshAt))
	require.False(t, id.IsNil(freshID))
	require.WithinDuration(t, time.Now(), freshAt, time.Minute)

	pending, err := PendingOutboxPayloadValues(ctx, aggregateType, "id")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"evt_1", "evt_2"}, pending)
	_, err = pool.Exec(ctx, `UPDATE sys_outbox SET status='published',published_at=NOW() WHERE id=$1`, fixed.ID)
	require.NoError(t, err)
	pending, err = PendingOutboxPayloadValues(ctx, aggregateType, "id")
	require.NoError(t, err)
	require.Equal(t, []string{"evt_2"}, pending)

	failing := OutboxRecord{AggregateType: aggregateType, AggregateID: id.New(), EventType: "record.batch", Payload: []byte(`{"id":"evt_3"}`)}
	broken := OutboxRecord{AggregateType: aggregateType, AggregateID: id.New(), EventType: "record.batch", Payload: []byte(`not json`)}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error { return EnqueueOutbox(ctx, failing, broken) })
	require.Error(t, err)
	var batched int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM sys_outbox WHERE aggregate_type=$1 AND event_type='record.batch'`, aggregateType).Scan(&batched))
	require.Zero(t, batched)
}
