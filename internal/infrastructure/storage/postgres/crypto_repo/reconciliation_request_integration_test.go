//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	dbassets "metapus/db"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

func reconciliationSnapshot(t *testing.T, f expectedRouteFixture, source id.ID) string {
	t.Helper()
	var snapshot string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(r)::text FROM reg_crypto_reconciliation_requests r WHERE source_id=$1`, source).Scan(&snapshot))
	return snapshot
}

func TestReconciliationRequestReplayPreservesCompletedProjection(t *testing.T) {
	f := newExpectedRouteFixture(t, "")
	r := reconciliationRequest{f.event.NetworkID, f.source, f.asset, "replay_test", id.New()}
	var workers sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		workers.Go(func() { errs <- enqueueReconciliationRequests(f.ctx, r) })
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	runtime := NewWorkerRuntimeRepo()
	requestID, wallet, claimed, err := runtime.ClaimReconciliationRequest(f.ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, r.wallet, wallet)
	require.NoError(t, runtime.FinishRequestedReconciliation(f.ctx, requestID, WorkerReconciliationStats{Acquired: true}, nil))
	before := reconciliationSnapshot(t, f, r.source)
	require.NoError(t, enqueueReconciliationRequests(f.ctx, r))
	require.Equal(t, before, reconciliationSnapshot(t, f, r.source))
	changed := r
	changed.wallet = f.destination
	require.ErrorContains(t, enqueueReconciliationRequests(f.ctx, changed), "another source target")
	// A later collision must roll back even the earlier new member of a batch.
	newRequest := reconciliationRequest{r.network, r.wallet, r.token, "a_new_request", id.New()}
	require.ErrorContains(t, enqueueReconciliationRequests(f.ctx, newRequest, changed), "another source target")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_reconciliation_requests`).Scan(&count))
	require.Equal(t, 1, count)
	for _, mutation := range []string{
		`UPDATE reg_crypto_reconciliation_requests SET wallet_id=$1`,
		`UPDATE reg_crypto_reconciliation_requests SET network_id=$1`,
		`UPDATE reg_crypto_reconciliation_requests SET token_id=$1`,
		`UPDATE reg_crypto_reconciliation_requests SET source_id=$1`,
		`UPDATE reg_crypto_reconciliation_requests SET id=$1`,
	} {
		_, err = f.pool.Exec(f.ctx, mutation, f.destination)
		require.ErrorContains(t, err, "identity is immutable")
	}
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_reconciliation_requests SET source_type='another source'`)
	require.ErrorContains(t, err, "identity is immutable")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_reconciliation_requests SET requested_at=requested_at+interval '1 second'`)
	require.ErrorContains(t, err, "identity is immutable")
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_reconciliation_requests`)
	require.ErrorContains(t, err, "cannot be deleted")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reconciliation_requests(network_id,wallet_id,token_id,source_type,source_id) VALUES($1,$2,$3,'native_funding_source',$4)`, r.network, r.wallet, r.token, id.New())
	require.ErrorContains(t, err, "does not match its source")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reconciliation_requests(network_id,wallet_id,token_id,source_type,source_id) VALUES($1,$2,$3,'generic_network_mismatch',$4)`, id.New(), r.wallet, r.token, id.New())
	require.ErrorContains(t, err, "does not match its source or asset network")
	require.Equal(t, before, reconciliationSnapshot(t, f, r.source))
}

func TestReconciliationNativeFundingPairUsesImmutableSource(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	_, fundingID := resourceFee(t, f, target, "20", "10")
	funding := crypto.NativeFunding{ID: fundingID, NetworkID: f.input.Request.NetworkID, NativeTokenID: f.input.Request.TokenID, SourceWalletID: f.input.WalletID, DestinationWalletID: target}
	wrong := funding
	wrong.DestinationWalletID = funding.SourceWalletID
	require.ErrorContains(t, (NativeFundingSettlementRepo{}).RequestNativeFundingReconciliation(f.ctx, wrong), "differs from its source target")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_reconciliation_requests`).Scan(&count))
	require.Zero(t, count, "neither member may survive a malformed pair")
	require.NoError(t, (NativeFundingSettlementRepo{}).RequestNativeFundingReconciliation(f.ctx, funding))
	require.NoError(t, (NativeFundingSettlementRepo{}).RequestNativeFundingReconciliation(f.ctx, funding))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_reconciliation_requests WHERE source_id=$1`, fundingID).Scan(&count))
	require.Equal(t, 2, count)
}

func TestReconciliationRequestWalletPrefixBeforeQueueRow(t *testing.T) {
	f := newExpectedRouteFixture(t, "")
	r := reconciliationRequest{f.event.NetworkID, f.source, f.asset, "lock_order_test", id.New()}
	require.NoError(t, enqueueReconciliationRequests(f.ctx, r))
	assertAccountingLockOrder(t, f.ctx, f.pool,
		func(ctx context.Context) error {
			return NewCustodyRecognitionRepo().LockWallets(ctx, f.source, f.destination)
		},
		func(ctx context.Context) error {
			_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `SELECT id FROM reg_crypto_reconciliation_requests WHERE source_id=$1 FOR UPDATE`, r.source)
			return err
		},
		func(ctx context.Context) error {
			return enqueueReconciliationRequests(ctx, r, reconciliationRequest{r.network, f.destination, r.token, "another_lock_order_test", id.New()})
		})
}

func TestReconciliationIdentityMigrationPreservesHistoryAndRejectsInvalidLegacyTarget(t *testing.T) {
	f := newExpectedRouteFixture(t, "")
	r := reconciliationRequest{f.event.NetworkID, f.source, f.asset, "migration_test", id.New()}
	require.NoError(t, enqueueReconciliationRequests(f.ctx, r))
	before := reconciliationSnapshot(t, f, r.source)
	db, err := sql.Open("pgx", f.pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, 91)
	require.NoError(t, err)
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	require.Equal(t, before, reconciliationSnapshot(t, f, r.source))
	_, err = provider.DownTo(f.ctx, 91)
	require.NoError(t, err)
	badSource := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reconciliation_requests(network_id,wallet_id,token_id,source_type,source_id) VALUES($1,$2,$3,'native_funding_source',$4)`, r.network, r.wallet, r.token, badSource)
	require.NoError(t, err)
	_, err = provider.Up(f.ctx)
	require.ErrorContains(t, err, "requires review before upgrade")
	var version int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
	require.Equal(t, 91, version)
	require.Equal(t, before, reconciliationSnapshot(t, f, r.source))
	// Remove only the synthetic bad row while still on schema 91.
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_reconciliation_requests WHERE source_id=$1`, badSource)
	require.NoError(t, err)
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	require.Equal(t, before, reconciliationSnapshot(t, f, r.source))
}
