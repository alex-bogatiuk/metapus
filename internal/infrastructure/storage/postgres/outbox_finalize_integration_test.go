//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

// stoppingOutboxHandler delivers the message and stops the relay while doing
// so, as a SIGTERM arriving mid-delivery would.
type stoppingOutboxHandler struct{ stop context.CancelFunc }

func (h stoppingOutboxHandler) Handle(context.Context, *OutboxMessage) error {
	h.stop()
	return nil
}

func publishOutboxFixture(t *testing.T, pool *pgxpool.Pool, aggregateType string) id.ID {
	t.Helper()
	txm := NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(context.Background(), txm)
	aggregateID := id.New()
	event := domain.DomainEvent{AggregateType: aggregateType, AggregateID: aggregateID,
		EventType: "finalize.test", Payload: map[string]string{"id": aggregateID.String()}}
	require.NoError(t, txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return NewOutboxPublisher().Publish(txCtx, event)
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sys_outbox WHERE aggregate_type=$1 AND aggregate_id=$2`, aggregateType, aggregateID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM sys_outbox_dlq WHERE aggregate_type=$1 AND aggregate_id=$2`, aggregateType, aggregateID)
	})
	return aggregateID
}

func TestOutboxDeliveredDuringShutdownIsMarkedPublished(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	aggregateID := publishOutboxFixture(t, pool, "TestFinalize")

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	relay := NewOutboxRelay(pool, 1, stoppingOutboxHandler{stop: stop}).FilterAggregates([]string{"TestFinalize"}, nil)
	processed, err := relay.ProcessBatch(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)

	// Left in 'processing', the message would be delivered again after its
	// lease expired.
	var status OutboxStatus
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT status FROM sys_outbox WHERE aggregate_type='TestFinalize' AND aggregate_id=$1`, aggregateID).Scan(&status))
	require.Equal(t, OutboxStatusPublished, status)
}

func TestOutboxFailedBeforeFiveAttemptsMovesToDLQ(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	aggregateID := publishOutboxFixture(t, pool, "TestEarlyDLQ")

	// A retry policy with a deadline gave up after the second attempt.
	_, err = pool.Exec(context.Background(), `
		UPDATE sys_outbox SET status='failed', retry_count=2, last_error='deadline passed'
		WHERE aggregate_type='TestEarlyDLQ' AND aggregate_id=$1`, aggregateID)
	require.NoError(t, err)

	moved, err := NewOutboxRelay(pool, 1, stoppingOutboxHandler{stop: func() {}}).MoveToDLQ(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, moved, int64(1))
	var retries int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT retry_count FROM sys_outbox_dlq WHERE aggregate_type='TestEarlyDLQ' AND aggregate_id=$1`, aggregateID).Scan(&retries))
	require.Equal(t, 2, retries)
}
