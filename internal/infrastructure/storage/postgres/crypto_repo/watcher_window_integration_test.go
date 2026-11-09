//go:build integration

package crypto_repo

import (
	"os"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestTimestampCheckpointProgressCASAndFencing(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	require.NotEmpty(t, dsn, "dedicated integration DB required")
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	network := id.New()
	key := "window-integration-" + id.Compact(network)
	repo := NewWatcherStateRepo()
	state, err := repo.Acquire(ctx, &crypto.WatcherState{NetworkID: network, WatcherKey: key, LastBlock: 10, LastTimestamp: 1000}, "owner-a")
	require.NoError(t, err)
	old := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	_, err = pool.Exec(ctx, `UPDATE sys_chain_watcher_state SET updated_at=$3 WHERE network_id=$1 AND watcher_key=$2`, network, key, old)
	require.NoError(t, err)
	require.NoError(t, repo.Save(ctx, state))
	require.True(t, state.UpdatedAt.Equal(old), "unchanged Save cannot conceal a stall")
	require.NoError(t, repo.Renew(ctx, state))
	require.NoError(t, repo.Release(ctx, state))
	newOwner, err := repo.Acquire(ctx, state, "owner-b")
	require.NoError(t, err)
	require.True(t, newOwner.UpdatedAt.Equal(old), "reacquiring a lease is not checkpoint progress")
	checkpoints, err := repo.ListIngestionCheckpoints(ctx, network)
	require.NoError(t, err)
	require.Len(t, checkpoints, 1)
	require.True(t, checkpoints[0].LastProgressAt.Equal(old))
	candidate := *newOwner
	candidate.LastTimestamp = 2000
	commit := crypto.WatcherWindowCommit{Candidate: candidate, ExpectedBlock: 10, ExpectedTimestamp: 1000}
	require.NoError(t, repo.CommitWindow(ctx, commit))
	persisted, err := repo.Get(ctx, network, key)
	require.NoError(t, err)
	require.EqualValues(t, 2000, persisted.LastTimestamp)
	require.True(t, persisted.UpdatedAt.After(old))
	require.ErrorIs(t, repo.CommitWindow(ctx, commit), crypto.ErrWatcherCheckpointConflict, "same fence cannot commit a stale expected cursor")
	commit.ExpectedTimestamp = 2000
	commit.Candidate.LastTimestamp = 3000
	commit.Candidate.LeaseOwner = state.LeaseOwner
	commit.Candidate.FencingToken = state.FencingToken
	require.ErrorIs(t, repo.CommitWindow(ctx, commit), crypto.ErrWatcherLeaseLost)
	persisted, err = repo.Get(ctx, network, key)
	require.NoError(t, err)
	require.EqualValues(t, 2000, persisted.LastTimestamp)
	commit.Candidate = *newOwner
	commit.Candidate.LastTimestamp = 1999
	require.ErrorIs(t, repo.CommitWindow(ctx, commit), crypto.ErrWatcherCheckpointConflict, "window cursor cannot regress")
}
