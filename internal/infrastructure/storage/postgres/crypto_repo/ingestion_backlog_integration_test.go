//go:build integration

package crypto_repo

import (
	"os"
	"testing"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestIngestionInboxAgeExcludesProcessedHistoryAndNeedsReview(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	require.NotEmpty(t, dsn, "disposable PostgreSQL required")
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE reg_crypto_chain_observations(network_id uuid,status text,received_at timestamptz)`)
	require.NoError(t, err)
	network, future := id.New(), id.New()
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_chain_observations VALUES
	 ($1,'processed',now()-interval '30 days'),($1,'pending',now()-interval '3 minutes'),
	 ($1,'pending',now()-interval '1 minute'),($1,'processing',now()-interval '2 minutes'),
	 ($1,'needs_review',now()-interval '1 day'),($2,'pending',now()+interval '1 minute')`, network, future)
	require.NoError(t, err)
	backlogs, err := NewChainEventInboxRepo().ListIngestionBacklogs(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), backlogs[network].Pending)
	require.Equal(t, int64(1), backlogs[network].Processing)
	require.Equal(t, int64(1), backlogs[network].NeedsReview)
	require.InDelta(t, 180, backlogs[network].OldestAgeSeconds, 5, "processed and needs_review ages are not queue waiting time")
	require.Zero(t, backlogs[future].OldestAgeSeconds)
	_, err = pool.Exec(ctx, `UPDATE reg_crypto_chain_observations SET status='processed'`)
	require.NoError(t, err)
	backlogs, err = NewChainEventInboxRepo().ListIngestionBacklogs(ctx)
	require.NoError(t, err)
	require.Empty(t, backlogs)
}
