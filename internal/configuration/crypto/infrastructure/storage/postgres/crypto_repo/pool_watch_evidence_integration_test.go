//go:build integration

package crypto_repo

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"

	"github.com/stretchr/testify/require"
)

func TestPoolCancellationReplayPreservesCommandAndHistory(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 2, 5)
	_, count, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	var first, second id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT min(id::text)::uuid,max(id::text)::uuid FROM reg_crypto_aml_pool_intents WHERE policy_id=$1`, p.ID).Scan(&first, &second))
	const reason = "target reduced before derivation"
	require.NoError(t, f.repo.CancelMerchantPoolIntent(f.ctx, first, reason, "operator"))
	snapshot := func() string {
		t.Helper()
		var data string
		require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(c)::text FROM reg_crypto_aml_pool_intent_cancellations c WHERE intent_id=$1`, first).Scan(&data))
		return data
	}
	before := snapshot()
	require.NoError(t, f.repo.CancelMerchantPoolIntent(f.ctx, first, reason, "operator"))
	for _, change := range [][2]string{{"another cancellation reason", "operator"}, {reason, "another operator"}} {
		err := f.repo.CancelMerchantPoolIntent(f.ctx, first, change[0], change[1])
		var appErr *apperror.AppError
		require.ErrorAs(t, err, &appErr)
		require.Equal(t, apperror.CodeConflict, appErr.Code)
		_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_pool_intent_cancellations(intent_id,reason,cancelled_by) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, first, change[0], change[1])
		require.ErrorContains(t, err, "original command")
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_pool_intent_cancellations(intent_id,reason,cancelled_by) VALUES($1,$2,'operator') ON CONFLICT DO NOTHING`, first, reason)
	require.NoError(t, err)
	for _, change := range [][2]any{{nil, "operator"}, {reason, nil}, {"  ", "operator"}} {
		_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_pool_intent_cancellations(intent_id,reason,cancelled_by) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, first, change[0], change[1])
		require.Error(t, err, "a conflict must not hide invalid evidence")
	}
	tm := postgres.MustGetTxManager(f.ctx)
	err = tm.RunInTransactionWithOptions(f.ctx, postgres.SerializableTxOptions(), func(ctx context.Context) error {
		return f.repo.CancelMerchantPoolIntent(ctx, first, reason, "operator")
	})
	require.ErrorContains(t, err, "read committed")
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_aml_pool_intent_cancellations WHERE intent_id=$1`, first)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_pool_intent_cancellations SET cancelled_by='rewrite' WHERE intent_id=$1`, first)
	require.Error(t, err)
	require.Equal(t, before, snapshot())

	// A failed enclosing operation must not leave a cancellation behind.
	err = tm.RunInTransaction(f.ctx, func(ctx context.Context) error {
		if err := f.repo.CancelMerchantPoolIntent(ctx, second, reason, "operator"); err != nil {
			return err
		}
		return apperror.NewConflict("synthetic enclosing operation rollback")
	})
	require.ErrorContains(t, err, "enclosing operation rollback")
	var exists bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intent_cancellations WHERE intent_id=$1)`, second).Scan(&exists))
	require.False(t, exists)
	actors := []string{"first concurrent operator", "second concurrent operator"}
	errs := make([]error, len(actors))
	var wg sync.WaitGroup
	for i := range actors {
		wg.Go(func() {
			if i == 0 {
				errs[i] = f.repo.CancelMerchantPoolIntent(f.ctx, second, reason, actors[i])
			} else {
				_, errs[i] = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_pool_intent_cancellations(intent_id,reason,cancelled_by) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, second, reason, actors[i])
			}
		})
	}
	wg.Wait()
	require.NotEqual(t, errs[0] == nil, errs[1] == nil, "exactly one command owns the cancellation")
	for i, err := range errs {
		if err == nil {
			require.NoError(t, f.repo.CancelMerchantPoolIntent(f.ctx, second, reason, actors[i]))
		} else {
			require.ErrorContains(t, err, "original command")
		}
	}
}

func TestPoolCancellationSQLLocksScopeBeforeIntent(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 1, 3)
	_, _, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	var intent id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM reg_crypto_aml_pool_intents WHERE policy_id=$1`, p.ID).Scan(&intent))
	tm := postgres.MustGetTxManager(f.ctx)
	assertAccountingLockOrder(t, f.ctx, f.pool,
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT token_id FROM sys_crypto_aml_routing_scopes WHERE network_id=$1 FOR UPDATE`, p.NetworkID)
			return err
		}, func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM reg_crypto_aml_pool_intents WHERE id=$1 FOR UPDATE`, intent)
			return err
		}, func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_aml_pool_intent_cancellations(intent_id,reason,cancelled_by) VALUES($1,'budget reduced','operator') ON CONFLICT DO NOTHING`, intent)
			return err
		})
}

func TestAddressCoverageRetainsFirstEvidenceAcrossRefreshAndLease(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 2, 5, crypto.ChainFamilyEVM)
	p.AutomaticBaseline = true
	_, err := f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: p, ExpectedVersion: p.Version, Actor: "operator"})
	require.NoError(t, err)
	_, _, err = f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	addresses := make([]string, 0, 2)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	for range 2 {
		claimed, wallet, err := service.RunNext(f.ctx, "coverage-control")
		require.NoError(t, err)
		require.True(t, claimed)
		var address string
		require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, wallet).Scan(&address))
		addresses = append(addresses, address)
	}
	coverage := crypto.AddressWatchCoverage{NetworkID: p.NetworkID, TokenID: f.input.Request.TokenID, WatcherKey: "erc20:test-contract", StartHeight: 40, LeaseOwner: "first watcher", FencingToken: 10}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_chain_watcher_state(network_id,watcher_key,lease_owner,fencing_token,lease_expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+interval '1 hour')`, coverage.NetworkID, coverage.WatcherKey, coverage.LeaseOwner, coverage.FencingToken)
	require.NoError(t, err)
	require.NoError(t, f.repo.RecordAMLAddressCoverage(f.ctx, coverage, []string{strings.ToUpper(addresses[0]), addresses[0]}))
	snapshot := func() string {
		t.Helper()
		var data string
		require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(c)::text FROM reg_crypto_aml_watch_coverage c JOIN cat_wallets w ON w.id=c.wallet_id WHERE w.address=$1`, addresses[0]).Scan(&data))
		return data
	}
	before := snapshot()
	coverage.StartHeight = 80
	require.NoError(t, f.repo.RecordAMLAddressCoverage(f.ctx, coverage, addresses[:1]))
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_chain_watcher_state SET lease_owner='second watcher',fencing_token=11 WHERE network_id=$1 AND watcher_key=$2`, coverage.NetworkID, coverage.WatcherKey)
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.RecordAMLAddressCoverage(f.ctx, coverage, addresses), "lease changed or expired")
	require.ErrorContains(t, f.repo.RecordAMLAddressCoverage(f.ctx, coverage, nil), "lease changed or expired")
	coverage.LeaseOwner, coverage.FencingToken = "second watcher", 11
	require.NoError(t, f.repo.RecordAMLAddressCoverage(f.ctx, coverage, addresses))
	coverage.StartHeight = 20 // A backscan cannot rewrite the original boundary.
	require.NoError(t, f.repo.RecordAMLAddressCoverage(f.ctx, coverage, addresses))
	require.Equal(t, before, snapshot())
	var count, late int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*),count(*) FILTER(WHERE start_height=80 AND lease_owner='second watcher' AND fencing_token=11) FROM reg_crypto_aml_watch_coverage`).Scan(&count, &late))
	require.Equal(t, 2, count)
	require.Equal(t, 1, late, "a newly added address gets its own later boundary")
	for _, change := range []func(*crypto.AddressWatchCoverage){
		func(c *crypto.AddressWatchCoverage) { c.NetworkID = id.New() },
		func(c *crypto.AddressWatchCoverage) { c.TokenID = id.New() },
		func(c *crypto.AddressWatchCoverage) { c.WatcherKey = "native_eth" },
		func(c *crypto.AddressWatchCoverage) { c.FencingToken-- },
	} {
		wrong := coverage
		change(&wrong)
		require.Error(t, f.repo.RecordAMLAddressCoverage(f.ctx, wrong, addresses))
	}
	require.Equal(t, before, snapshot())
	var movements int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements)+(SELECT count(*) FROM reg_crypto_merchant_balance_movements)`).Scan(&movements))
	require.Zero(t, movements, "watch registration never posts money")
}

func TestAddressCoverageRejectsLeaseExpiredDuringLockWait(t *testing.T) {
	f, _, wallet := prepareAutomaticBaseline(t, crypto.ChainFamilyEVM)
	coverage := crypto.AddressWatchCoverage{NetworkID: f.input.Request.NetworkID, TokenID: f.input.Request.TokenID, WatcherKey: "erc20:test-contract", StartHeight: 40, LeaseOwner: "watcher", FencingToken: 10}
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, wallet).Scan(&address))
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_chain_watcher_state(network_id,watcher_key,lease_owner,fencing_token,lease_expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+interval '1 hour')`, coverage.NetworkID, coverage.WatcherKey, coverage.LeaseOwner, coverage.FencingToken)
	require.NoError(t, err)
	tm := postgres.MustGetTxManager(f.ctx)
	for _, raw := range []bool{false, true} {
		t.Run(map[bool]string{false: "repository", true: "direct SQL"}[raw], func(t *testing.T) {
			_, err := f.pool.Exec(f.ctx, `UPDATE sys_chain_watcher_state SET lease_expires_at=clock_timestamp()+interval '1 hour' WHERE network_id=$1 AND watcher_key=$2`, coverage.NetworkID, coverage.WatcherKey)
			require.NoError(t, err)
			result := make(chan error, 1)
			assertAccountingLockOrder(t, f.ctx, f.pool,
				func(ctx context.Context) error {
					_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT network_id FROM sys_chain_watcher_state WHERE network_id=$1 AND watcher_key=$2 FOR UPDATE`, coverage.NetworkID, coverage.WatcherKey)
					return err
				}, func(ctx context.Context) error {
					// Later than the waiting transaction's NOW(), already expired
					// when its row lock is finally acquired.
					_, err := tm.GetQuerier(ctx).Exec(ctx, `UPDATE sys_chain_watcher_state SET lease_expires_at=clock_timestamp() WHERE network_id=$1 AND watcher_key=$2`, coverage.NetworkID, coverage.WatcherKey)
					return err
				}, func(ctx context.Context) error {
					if raw {
						opts := postgres.DefaultTxOptions()
						opts.UseSavepoint = true
						err := tm.RunInTransactionWithOptions(ctx, opts, func(ctx context.Context) error {
							_, err := tm.GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_aml_watch_coverage(wallet_id,token_id,network_id,watcher_key,start_height,lease_owner,fencing_token) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, wallet, coverage.TokenID, coverage.NetworkID, coverage.WatcherKey, coverage.StartHeight, coverage.LeaseOwner, coverage.FencingToken)
							return err
						})
						result <- err
					} else {
						result <- f.repo.RecordAMLAddressCoverage(ctx, coverage, []string{address})
					}
					return nil
				})
			require.Error(t, <-result)
		})
	}
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_aml_watch_coverage`).Scan(&count))
	require.Zero(t, count)
}
