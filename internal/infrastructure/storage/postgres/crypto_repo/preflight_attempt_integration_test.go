//go:build integration

package crypto_repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

func preflightSnapshot108(t *testing.T, f amlRoutingFixture) string {
	t.Helper()
	var data string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'jobs',(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM reg_crypto_aml_preflight_jobs j),
 'attempts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY lease_token) FROM reg_crypto_aml_preflight_attempts a),
 'observations',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM reg_crypto_aml_preflight_observations o))::text`).Scan(&data))
	return data
}

func TestPreflightAttemptFreezesIdentityAndRetainsEachPolicyBasis(t *testing.T) {
	f, p, _ := prepareAutomaticBaseline(t)
	first, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "first worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	before := preflightSnapshot108(t, f)
	for _, change := range []func(*crypto.AMLPreflightJob){
		func(j *crypto.AMLPreflightJob) { j.PolicyID = id.New() },
		func(j *crypto.AMLPreflightJob) { j.MerchantID = id.New() },
		func(j *crypto.AMLPreflightJob) { j.NetworkID = id.New() },
		func(j *crypto.AMLPreflightJob) { j.WalletID = id.New() },
		func(j *crypto.AMLPreflightJob) { j.TokenID = id.New(); j.Asset.TokenID = j.TokenID },
		func(j *crypto.AMLPreflightJob) { j.ScopeVersion++ },
		func(j *crypto.AMLPreflightJob) { j.PolicyVersion++ },
		func(j *crypto.AMLPreflightJob) { j.Asset.Decimals++ },
		func(j *crypto.AMLPreflightJob) { j.Address = "another address" },
	} {
		wrong := first
		change(&wrong)
		require.ErrorContains(t, f.repo.RetryAMLPreflight(f.ctx, wrong, "checkpoint_rejected"), "recorded basis")
		require.ErrorContains(t, f.repo.SaveAMLPreflight(f.ctx, wrong, checkpointForJob(wrong, "0")), "recorded basis")
	}
	require.Equal(t, before, preflightSnapshot108(t, f))
	for _, mutation := range []string{
		`UPDATE reg_crypto_aml_preflight_jobs SET id=$2 WHERE id=$1`,
		`UPDATE reg_crypto_aml_preflight_jobs SET wallet_id=$2 WHERE id=$1`,
		`UPDATE reg_crypto_aml_preflight_jobs SET token_id=$2 WHERE id=$1`,
		`UPDATE reg_crypto_aml_preflight_jobs SET policy_id=$2 WHERE id=$1`,
	} {
		_, err := f.pool.Exec(f.ctx, mutation, first.ID, id.New())
		require.ErrorContains(t, err, "identity is immutable")
	}
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_preflight_jobs SET scope_version=scope_version+1 WHERE id=$1`, first.ID)
	require.ErrorContains(t, err, "active attempt basis")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_preflight_attempts SET basis='{}' WHERE lease_token=$1`, first.LeaseToken)
	require.ErrorContains(t, err, "append-only")
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_aml_preflight_attempts WHERE lease_token=$1`, first.LeaseToken)
	require.ErrorContains(t, err, "append-only")
	require.NoError(t, f.repo.RetryAMLPreflight(f.ctx, first, "checkpoint_unavailable"))
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_preflight_jobs j SET status='processing',lease_token=a.lease_token,lease_expires_at=a.lease_expires_at,worker_id=a.worker_id,attempts=j.attempts+1
 FROM reg_crypto_aml_preflight_attempts a WHERE j.id=$1 AND a.lease_token=$2`, first.ID, first.LeaseToken)
	require.ErrorContains(t, err, "token cannot be reused")
	p.TargetReady = 2
	p, err = f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: p, ExpectedVersion: p.Version, Actor: "operator"})
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_preflight_jobs SET next_attempt_at=clock_timestamp() WHERE id=$1`, first.ID)
	require.NoError(t, err)
	second, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "second worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, first.ID, second.ID)
	require.NotEqual(t, first.LeaseToken, second.LeaseToken)
	require.Equal(t, p.Version, second.PolicyVersion)
	require.Greater(t, second.PolicyVersion, first.PolicyVersion)
	var initial int64
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT initial_policy_version FROM reg_crypto_aml_preflight_jobs WHERE id=$1`, first.ID).Scan(&initial))
	require.Equal(t, first.PolicyVersion, initial)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_aml_preflight_attempts WHERE job_id=$1`, first.ID).Scan(&count))
	require.Equal(t, 2, count)
	current := preflightSnapshot108(t, f)
	require.Error(t, f.repo.RetryAMLPreflight(f.ctx, first, "checkpoint_rejected"))
	require.Error(t, f.repo.SaveAMLPreflight(f.ctx, first, checkpointForJob(first, "0")))
	require.Equal(t, current, preflightSnapshot108(t, f))
	require.NoError(t, f.repo.SaveAMLPreflight(f.ctx, second, checkpointForJob(second, "0")))
}

func TestPreflightAttemptExpiryAndPolicyChangeCannotPublishStaleProof(t *testing.T) {
	f, p, _ := prepareAutomaticBaseline(t)
	first, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "short worker", time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Eventually(t, func() bool {
		var expired bool
		err := f.pool.QueryRow(f.ctx, `SELECT lease_expires_at<=clock_timestamp() FROM reg_crypto_aml_preflight_jobs WHERE id=$1`, first.ID).Scan(&expired)
		return err == nil && expired
	}, 3*time.Second, 20*time.Millisecond)
	require.ErrorContains(t, f.repo.SaveAMLPreflight(f.ctx, first, checkpointForJob(first, "0")), "expired or changed")
	require.ErrorContains(t, f.repo.RetryAMLPreflight(f.ctx, first, "checkpoint_rejected"), "expired or changed")
	second, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "replacement worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	before := preflightSnapshot108(t, f)
	require.Error(t, f.repo.RetryAMLPreflight(f.ctx, first, "checkpoint_unavailable"))
	require.Equal(t, before, preflightSnapshot108(t, f))
	p.AutomaticBaseline = false
	_, err = f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: p, ExpectedVersion: p.Version, Actor: "operator"})
	require.NoError(t, err)
	require.Error(t, f.repo.SaveAMLPreflight(f.ctx, second, checkpointForJob(second, "0")))
	// A failed old-policy attempt may release only its own still-current lease.
	require.NoError(t, f.repo.RetryAMLPreflight(f.ctx, second, "checkpoint_rejected"))
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_aml_preflight_observations`).Scan(&count))
	require.Zero(t, count)
}

func TestPreflightClaimRequiresAttemptAtCommitAndSkipsBusyWallet(t *testing.T) {
	f, _, wallet := prepareAutomaticBaseline(t)
	job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, f.repo.RetryAMLPreflight(f.ctx, job, "checkpoint_unavailable"))
	before := preflightSnapshot108(t, f)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_preflight_jobs SET status='processing',lease_token=$2,lease_expires_at=clock_timestamp()+interval '1 minute',worker_id='unrecorded',attempts=attempts+1 WHERE id=$1`, job.ID, id.New())
	require.ErrorContains(t, err, "immutable attempt evidence")
	require.Equal(t, before, preflightSnapshot108(t, f))
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_preflight_jobs SET next_attempt_at=clock_timestamp() WHERE id=$1`, job.ID)
	require.NoError(t, err)
	tx, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(f.ctx) }()
	_, err = tx.Exec(f.ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, wallet)
	require.NoError(t, err)
	_, ok, err = f.repo.ClaimAMLPreflight(f.ctx, "busy observer", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
	_, err = tx.Exec(f.ctx, `SELECT id FROM reg_crypto_aml_preflight_jobs WHERE id=$1 FOR UPDATE NOWAIT`, job.ID)
	require.NoError(t, err, "selection must not retain a job while waiting for its wallet")
	require.NoError(t, tx.Rollback(f.ctx))
	_, ok, err = f.repo.ClaimAMLPreflight(f.ctx, "resumed worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestPreflightAttemptMigrationRetainsLegacyJobWithoutInventedBasis(t *testing.T) {
	f, p, wallet := prepareAutomaticBaseline(t)
	provider := history107Provider(t, f)
	_, err := provider.DownTo(f.ctx, 100)
	require.NoError(t, err)
	legacy := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_preflight_jobs(id,wallet_id,token_id,policy_id,policy_version,scope_version) VALUES($1,$2,$3,$4,$5,1)`, legacy, wallet, f.input.Request.TokenID, p.ID, p.Version)
	require.NoError(t, err)
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, 100)
	require.NoError(t, err, "untouched legacy job permits Down/Up")
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "first recorded worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, legacy, job.ID)
	var unknown bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT initial_policy_version IS NULL AND initial_scope_version IS NULL FROM reg_crypto_aml_preflight_jobs WHERE id=$1`, legacy).Scan(&unknown))
	require.True(t, unknown)
	_, err = provider.DownTo(f.ctx, 100)
	require.ErrorContains(t, err, "forward recovery")
}

// Set up the second network in the same database so queue selection must choose
// between a chronologically earlier unready network and a later ready network.
func secondPoolNetwork108(t *testing.T, f amlRoutingFixture) (amlRoutingFixture, crypto.MerchantPoolPolicy, id.ID) {
	t.Helper()
	input, unused := seedAMLJob(t, f.ctx, f.pool)
	other := amlRoutingFixture{ctx: f.ctx, pool: f.pool, input: input, repo: f.repo}
	_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_jobs SET status='needs_attention' WHERE id=$1`, unused)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_custody_policies(network_id,enforced) VALUES($1,TRUE)`, input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version)
 VALUES($1,$2,$3,TRUE,'routing-v1',$4,'test-mapping-v1')`, input.Request.MerchantID, input.Request.NetworkID, input.Request.TokenID, input.ProviderAccountID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_blockchain_networks SET network_identifier='mainnet' WHERE id=$1`, input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET enabled=TRUE,max_total_wallets=100,derivation_namespace='m/44''/195''/0''/0',needs_review=FALSE WHERE network_id=$1`, input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_wallet_derivation_cursors(network_id,derivation_namespace,next_index,needs_review) VALUES($1,'m/44''/195''/0''/0',100,FALSE)`, input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET is_active=FALSE WHERE id=$1`, input.WalletID)
	require.NoError(t, err)
	native := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,$1::uuid::text,'Activation native',$2,'TRX',6,'native')`, native, input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_provider_mappings(network_id,token_id,version,contract_version,contract_address,currency,provider_token_id,native,decimal_places,capability_accepted,acceptance_reference,currencylist_snapshot,pending_statuses)
 VALUES($1,$2,'activation-native','synthetic-v1','','TRX','',TRUE,6,TRUE,'fixture','{"synthetic":true}',ARRAY['PENDING'])`, input.Request.NetworkID, native)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version)
 VALUES($1,$2,$3,TRUE,'routing-v1',$4,'activation-native')`, input.Request.MerchantID, input.Request.NetworkID, native, input.ProviderAccountID)
	require.NoError(t, err)
	p, err := f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: crypto.MerchantPoolPolicy{ID: id.New(), MerchantID: input.Request.MerchantID, NetworkID: input.Request.NetworkID,
		Role: "network_fee", Enabled: true, TargetReady: 1, MaxTotal: 3, MaxProvisionBatch: 1, AutomaticBaseline: true}, Actor: "operator"})
	require.NoError(t, err)
	_, _, err = f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	ok, wallet, err := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t)).RunNext(f.ctx, "second network")
	require.NoError(t, err)
	require.True(t, ok)
	return other, p, wallet
}

func TestPoolActivationQueueSkipsUnreadyEarlierNetworkAndKeepsExistingTask(t *testing.T) {
	f, _, firstWallet := prepareAutomaticBaseline(t)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_pool_policies SET next_reconcile_at=clock_timestamp()+interval '1 day'`)
	require.NoError(t, err)
	other, _, ready := secondPoolNetwork108(t, f)
	for range 3 {
		job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "activation preflight", time.Minute)
		require.NoError(t, err)
		if !ok {
			break
		}
		amount := "0"
		if job.WalletID == firstWallet {
			amount = "1"
		}
		require.NoError(t, f.repo.SaveAMLPreflight(f.ctx, job, checkpointForJob(job, amount)))
	}
	other.rebuild(t)
	released, err := f.repo.ReleaseNextMerchantPoolBaselineHold(f.ctx)
	require.NoError(t, err)
	require.True(t, released)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_by='integration-test',resolution='independent empty preflight registered',resolved_at=clock_timestamp()
 WHERE wallet_id=$1 AND owner_type='wallet_reconciliation' AND status='open'
 AND EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=$1)`, ready)
	require.NoError(t, err)
	var eligible int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*)`+amlPoolActivationCandidates).Scan(&eligible))
	require.Equal(t, 1, eligible)
	var queued bool
	tm := postgres.MustGetTxManager(f.ctx)
	assertAccountingLockOrder(t, f.ctx, f.pool, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT token_id FROM sys_crypto_aml_routing_scopes WHERE network_id=$1 FOR UPDATE`, other.input.Request.NetworkID)
		return err
	}, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, ready)
		return err
	}, func(ctx context.Context) error {
		var err error
		queued, err = f.repo.QueueNextAMLPoolActivation(ctx)
		return err
	})
	require.True(t, queued)
	var target id.ID
	var original string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT wallet_id,to_jsonb(a)::text FROM reg_crypto_account_activations a`).Scan(&target, &original))
	require.Equal(t, ready, target)
	queued, err = f.repo.QueueNextAMLPoolActivation(f.ctx)
	require.NoError(t, err)
	require.False(t, queued)
	var after string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(a)::text FROM reg_crypto_account_activations a`).Scan(&after))
	require.Equal(t, original, after)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_balance_movements`).Scan(&count))
	require.Zero(t, count)
}

func TestPreflightSaveUsesCompleteScopePrefixBeforeJob(t *testing.T) {
	f, _, wallet := prepareAutomaticBaseline(t)
	job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "prefix worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	tx, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(f.ctx) }()
	_, err = tx.Exec(f.ctx, `SELECT token_id FROM sys_crypto_aml_routing_scopes WHERE network_id=$1 FOR UPDATE`, job.NetworkID)
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.SaveAMLPreflight(f.ctx, job, checkpointForJob(job, "0")), "55P03")
	_, err = tx.Exec(f.ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE NOWAIT`, wallet)
	require.NoError(t, err)
	_, err = tx.Exec(f.ctx, `SELECT id FROM reg_crypto_aml_preflight_jobs WHERE id=$1 FOR UPDATE NOWAIT`, job.ID)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(f.ctx))
	require.NoError(t, postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		return f.repo.SaveAMLPreflight(ctx, job, checkpointForJob(job, "0"))
	}))
}
