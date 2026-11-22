//go:build integration

package crypto_repo

import (
	"metapus/pkg/sdk/id"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAMLPreflightOperatorRetryPreservesHistoryAndNeverOverridesInventory(t *testing.T) {
	f, policy := automaticPoolFixture(t, "merchant_pool", 1, 3)
	_, _, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	var intent id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM reg_crypto_aml_pool_intents WHERE policy_id=$1 LIMIT 1`, policy.ID).Scan(&intent))
	// The pending job uses the already-owned fixture wallet and a real enabled scope.
	job := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_preflight_jobs(id,wallet_id,token_id,policy_id,policy_version,scope_version,status,attempts,error_code) VALUES($1,$2,$3,$4,1,1,'needs_attention',120,'checkpoint_unavailable')`, job, f.input.WalletID, f.input.Request.TokenID, policy.ID)
	require.NoError(t, err)
	require.NoError(t, f.repo.RetryExhaustedAMLPreflight(f.ctx, job, "RPC checkpoint reader repaired", "operator"))
	require.Error(t, f.repo.RetryExhaustedAMLPreflight(f.ctx, job, "duplicate request", "operator"))
	var attempts, audits int
	var status string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT attempts,status FROM reg_crypto_aml_preflight_jobs WHERE id=$1`, job).Scan(&attempts, &status))
	require.Zero(t, attempts)
	require.Equal(t, "retry_wait", status)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_event_log WHERE entity_id=$1 AND details->>'previousAttempts'='120'`, job).Scan(&audits))
	require.Equal(t, 1, audits)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_preflight_jobs SET status='needs_attention',attempts=120,error_code='preflight_inventory_detected' WHERE id=$1`, job)
	require.NoError(t, err)
	require.Error(t, f.repo.RetryExhaustedAMLPreflight(f.ctx, job, "must not clear inventory hold", "operator"))
}
