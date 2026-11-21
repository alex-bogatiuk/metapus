//go:build integration

package crypto_repo

import (
	"context"
	"fmt"
	"os"
	"testing"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestWatcherBlockCommitAtomicRollbackAndFencing(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn, "dedicated integration DB required")
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	network := id.New()
	suffix := id.Compact(network)
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,confirmations_needed,is_active)
 VALUES($1,$2,'Atomic watcher integration','evm',$3,'ETH',18,12,FALSE)`, network, "WCI-"+suffix, "wci-"+suffix)
	require.NoError(t, err)
	repo := NewWatcherStateRepo()
	for _, failure := range []string{"history", "checkpoint", "prune", "commit", "none"} {
		t.Run(failure, func(t *testing.T) {
			key := "atomic-" + failure + suffix
			state, err := repo.Acquire(ctx, &crypto.WatcherState{NetworkID: network, WatcherKey: key}, "owner-a")
			require.NoError(t, err)
			require.NoError(t, repo.SaveBlockFingerprint(ctx, network, key, crypto.WatcherBlockFingerprint{BlockNumber: 0, BlockHash: "genesis"}, 0))
			if failure != "none" {
				function := "watcher_inject_" + failure + suffix
				table := "sys_chain_watcher_block_history"
				operation := "INSERT"
				prefix := "CREATE TRIGGER"
				if failure == "checkpoint" {
					table = "sys_chain_watcher_state"
					operation = "UPDATE"
				}
				if failure == "prune" {
					operation = "DELETE"
				}
				timing := "BEFORE"
				deferred := ""
				if failure == "commit" {
					table = "sys_chain_watcher_state"
					operation = "UPDATE"
					timing = "AFTER"
					prefix = "CREATE CONSTRAINT TRIGGER"
					deferred = "DEFERRABLE INITIALLY DEFERRED"
				}
				_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
     IF COALESCE(NEW.watcher_key,OLD.watcher_key)='%s' THEN RAISE EXCEPTION 'injected %s failure'; END IF;
     RETURN COALESCE(NEW,OLD); END $$`, function, key, failure))
				require.NoError(t, err)
				_, err = pool.Exec(ctx, fmt.Sprintf(`%s %s %s %s ON %s %s FOR EACH ROW EXECUTE FUNCTION %s()`, prefix, function, timing, operation, table, deferred, function))
				require.NoError(t, err)
				defer func() {
					_, dropErr := pool.Exec(context.WithoutCancel(ctx), fmt.Sprintf(`DROP TRIGGER %s ON %s; DROP FUNCTION %s()`, function, table, function))
					require.NoError(t, dropErr)
				}()
			}
			candidate := *state
			candidate.LastBlock = 1
			candidate.LastTimestamp = 100
			candidate.Fingerprint = "block-1"
			command := crypto.WatcherBlockCommit{Candidate: candidate, ExpectedBlock: 0, ExpectedHash: "", Block: crypto.WatcherBlockFingerprint{BlockNumber: 1, BlockHash: "block-1", Timestamp: 100}, RetainFrom: 1}
			err = repo.CommitBlock(ctx, command)
			persisted, readErr := repo.Get(ctx, network, key)
			require.NoError(t, readErr)
			_, hasBlock, readErr := repo.GetBlockFingerprint(ctx, network, key, 1)
			require.NoError(t, readErr)
			_, hasGenesis, readErr := repo.GetBlockFingerprint(ctx, network, key, 0)
			require.NoError(t, readErr)
			if failure != "none" {
				require.ErrorContains(t, err, "injected")
				require.Zero(t, persisted.LastBlock)
				require.False(t, hasBlock)
				require.True(t, hasGenesis)
				return
			}
			require.NoError(t, err)
			require.Equal(t, int64(1), persisted.LastBlock)
			require.True(t, hasBlock)
			require.False(t, hasGenesis)
			require.ErrorIs(t, repo.CommitBlock(ctx, command), crypto.ErrWatcherCheckpointConflict, "replay cannot overwrite expected checkpoint")
			require.NoError(t, repo.Release(ctx, persisted))
			newOwner, err := repo.Acquire(ctx, persisted, "owner-b")
			require.NoError(t, err)
			require.Greater(t, newOwner.FencingToken, persisted.FencingToken)
			command.Candidate.LastBlock = 2
			command.Candidate.LastTimestamp = 101
			command.Candidate.Fingerprint = "block-2"
			command.ExpectedBlock = 1
			command.ExpectedHash = "block-1"
			command.Block = crypto.WatcherBlockFingerprint{BlockNumber: 2, BlockHash: "block-2", Timestamp: 101}
			require.ErrorIs(t, repo.CommitBlock(ctx, command), crypto.ErrWatcherLeaseLost, "stale owner/fence must not commit")
			command.Candidate.LeaseOwner = newOwner.LeaseOwner
			command.Candidate.FencingToken = newOwner.FencingToken
			command.ExpectedHash = "wrong"
			require.ErrorIs(t, repo.CommitBlock(ctx, command), crypto.ErrWatcherCheckpointConflict)
			command.ExpectedHash = "block-1"
			require.NoError(t, repo.CommitBlock(ctx, command))
			command.Candidate.LastBlock = 4
			require.ErrorIs(t, repo.CommitBlock(ctx, command), crypto.ErrWatcherCheckpointConflict, "gaps rejected before DB mutation")
		})
	}
}
