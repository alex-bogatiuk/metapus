//go:build integration

package crypto_repo

import (
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAMLAutomaticSweepPlannerReservesOneOwnedBatchUnderConcurrency(t *testing.T) {
	f, batch, _ := batchFixture(t, "200", 600)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=1000 WHERE network_id=$1;`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET status='sweep_pending',tier='pool',wallet_role='merchant_pool',chain_activation_status='active' WHERE id=$1`, f.input.WalletID)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make([]bool, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Go(func() {
			results[i], errs[i] = cryptoapp.NewAMLBatchSweepPlanner(f.repo, tenant.ContextTxManager{}).RunOnce(f.ctx)
		})
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.NotEqual(t, results[0], results[1])
	var count int
	var dest id.ID
	var principal, fee, status string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_sweep_attempts WHERE wallet_id=$1`, f.input.WalletID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT destination_wallet_id,principal::text,service_fee::text,status FROM reg_crypto_aml_internal_allocations WHERE source_wallet_id=$1`, f.input.WalletID).Scan(&dest, &principal, &fee, &status))
	require.Equal(t, batch.CollectorID, dest)
	require.Equal(t, "90", principal)
	require.Equal(t, "10", fee)
	require.Equal(t, "reserved", status)
}

func TestAMLAutomaticSweepPlannerPreservesHighAndBlockedSource(t *testing.T) {
	f, _, _ := batchFixture(t, "200", 600)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=1000 WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET status='sweep_pending',tier='pool',wallet_role='merchant_pool',chain_activation_status='active' WHERE id=$1`, f.input.WalletID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_risk_states SET risk_class='high' WHERE wallet_id=$1 AND token_id=$2`, f.input.WalletID, f.input.Request.TokenID)
	require.NoError(t, err)
	planned, err := cryptoapp.NewAMLBatchSweepPlanner(f.repo, tenant.ContextTxManager{}).RunOnce(f.ctx)
	require.NoError(t, err)
	require.False(t, planned)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_internal_allocations`).Scan(&count))
	require.Zero(t, count)
}
