//go:build integration

package crypto_repo

import (
	"strings"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

func prepareAutomaticBaseline(t *testing.T, family ...crypto.ChainFamily) (amlRoutingFixture, crypto.MerchantPoolPolicy, id.ID) {
	t.Helper()
	f, p := automaticPoolFixture(t, "merchant_pool", 1, 3, family...)
	p.AutomaticBaseline = true
	var err error
	p, err = f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: p, ExpectedVersion: p.Version, Actor: "operator"})
	require.NoError(t, err)
	_, _, err = f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	claimed, wallet, err := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t)).RunNext(f.ctx, "control")
	require.NoError(t, err)
	require.True(t, claimed)
	return f, p, wallet
}
func checkpointForJob(job crypto.AMLPreflightJob, amount string) crypto.EmptyCheckpoint {
	hash := strings.Repeat("a", 64)
	return crypto.EmptyCheckpoint{NetworkID: job.NetworkID, Asset: job.Asset, Address: job.Address, Height: 30, Hash: hash, ObservedAt: time.Now().UTC(), Sources: []crypto.EmptyCheckpointSource{{ProviderID: "one", TrustGroup: "first", Height: 30, Hash: hash, Balance: types.MustRawAmount(amount), Method: "solidified_bracket"}, {ProviderID: "two", TrustGroup: "second", Height: 30, Hash: hash, Balance: types.MustRawAmount(amount), Method: "solidified_bracket"}}}
}
func TestAMLAutomaticBaselineQuorumAndLeaseFence(t *testing.T) {
	f, p, wallet := prepareAutomaticBaseline(t)
	job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, wallet, job.WalletID)
	proof := checkpointForJob(job, "0")
	proof.ObservedAt = time.Now().UTC().Add(500 * time.Millisecond)
	stale := job
	stale.LeaseToken = id.New()
	require.Error(t, f.repo.SaveAMLPreflight(f.ctx, stale, proof))
	require.NoError(t, f.repo.SaveAMLPreflight(f.ctx, job, proof))
	require.Error(t, f.repo.SaveAMLPreflight(f.ctx, job, proof), "ack replay never creates a second baseline")
	var baseline, observation int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=$1`, wallet).Scan(&baseline))
	require.Equal(t, 1, baseline)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_preflight_observations WHERE wallet_id=$1`, wallet).Scan(&observation))
	require.Equal(t, 1, observation)
	f.rebuild(t)
	released, err := f.repo.ReleaseNextMerchantPoolBaselineHold(f.ctx)
	require.NoError(t, err)
	require.True(t, released)
	// Baseline does not claim chain activation or change the financial balance.
	stats, err := f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Zero(t, stats.Ready)
	require.Equal(t, 1, stats.Pending)
}
func TestAMLAutomaticBaselineNonemptyIsTerminalEvidence(t *testing.T) {
	f, _, wallet := prepareAutomaticBaseline(t)
	job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, f.repo.SaveAMLPreflight(f.ctx, job, checkpointForJob(job, "1")))
	_, ok, err = f.repo.ClaimAMLPreflight(f.ctx, "worker", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
	var state string
	var baseline, held int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_aml_preflight_jobs WHERE id=$1`, job.ID).Scan(&state))
	require.Equal(t, "needs_attention", state)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=$1`, wallet).Scan(&baseline))
	require.Zero(t, baseline)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND reason='aml_preflight_inventory_detected' AND status='open'`, wallet).Scan(&held))
	require.Equal(t, 1, held)
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_aml_preflight_observations WHERE job_id=$1`, job.ID)
	require.Error(t, err)
}
func TestAMLAutomaticBaselinePolicyChangeRejectsInflightProof(t *testing.T) {
	f, p, _ := prepareAutomaticBaseline(t)
	job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	p.AutomaticBaseline = false
	_, err = f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: p, ExpectedVersion: p.Version, Actor: "operator"})
	require.NoError(t, err)
	require.Error(t, f.repo.SaveAMLPreflight(f.ctx, job, checkpointForJob(job, "0")))
}

func TestAMLAutomaticBaselineEVMRequiresAssetCoverageBeforeCheckpoint(t *testing.T) {
	f, _, wallet := prepareAutomaticBaseline(t, crypto.ChainFamilyEVM)
	job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	proof := checkpointForJob(job, "0")
	for i := range proof.Sources {
		proof.Sources[i].Method = "finalized_block_bracket"
	}
	require.Error(t, f.repo.SaveAMLPreflight(f.ctx, job, proof), "a zero balance alone does not prove address coverage")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_chain_watcher_state(network_id,watcher_key,lease_owner,fencing_token,lease_expires_at) VALUES($1,'erc20:test-contract','active-watcher',10,NOW()+interval '1 hour')`, job.NetworkID)
	require.NoError(t, err)
	coverage := crypto.AddressWatchCoverage{NetworkID: job.NetworkID, TokenID: job.TokenID, WatcherKey: "erc20:test-contract", StartHeight: 31, LeaseOwner: "stale-watcher", FencingToken: 9}
	require.ErrorContains(t, f.repo.RecordAMLAddressCoverage(f.ctx, coverage, []string{job.Address}), "lease changed or expired")
	var recorded int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_watch_coverage WHERE wallet_id=$1`, wallet).Scan(&recorded))
	require.Zero(t, recorded, "another replica cannot attest the active watcher's address set")
	coverage.LeaseOwner, coverage.FencingToken = "active-watcher", 10
	require.NoError(t, f.repo.RecordAMLAddressCoverage(f.ctx, coverage, []string{job.Address}))
	require.Error(t, f.repo.SaveAMLPreflight(f.ctx, job, proof), "finality must catch up to address registration")
	proof.Height = 31
	for i := range proof.Sources {
		proof.Sources[i].Height = 31
	}
	require.NoError(t, f.repo.SaveAMLPreflight(f.ctx, job, proof))
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_aml_watch_coverage WHERE wallet_id=$1`, wallet)
	require.Error(t, err, "coverage barriers remain immutable")
}
