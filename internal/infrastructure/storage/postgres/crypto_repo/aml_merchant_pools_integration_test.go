//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbassets "metapus/db"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func automaticPoolFixture(t *testing.T, role string, target, maxTotal int, family ...crypto.ChainFamily) (amlRoutingFixture, crypto.MerchantPoolPolicy) {
	t.Helper()
	f, _, _ := merchantProvisionFixture(t, family...)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET enabled=TRUE WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET is_active=FALSE WHERE id=$1`, f.input.WalletID)
	require.NoError(t, err)
	p := crypto.MerchantPoolPolicy{ID: id.New(), MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID, Role: role, Enabled: true, TargetReady: target, MaxTotal: maxTotal, MaxProvisionBatch: 3}
	if role == "network_fee" {
		p.MaxProvisionBatch = 1
	}
	if role == "liquidity_hot" {
		p.TokenID = &f.input.Request.TokenID
		p.RiskClass = "low"
		_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_network_policies(network_id) VALUES($1) ON CONFLICT DO NOTHING`, p.NetworkID)
		require.NoError(t, err)
		_, err = f.repo.ConfigureAMLBatchPolicy(f.ctx, aml.BatchPolicyRequest{MerchantID: p.MerchantID, NetworkID: p.NetworkID, TokenID: *p.TokenID, Class: aml.Low, MaxGross: types.MustRawAmount("100"), MaxAgeSeconds: 600, Actor: "operator"})
		require.NoError(t, err)
	}
	p, err = f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: p, Actor: "operator"})
	require.NoError(t, err)
	return f, p
}

func automaticPoolVault(t *testing.T) merchantProvisionStub {
	t.Helper()
	return merchantProvisionStub(func(ctx context.Context, _ id.ID, n crypto.NetworkRef, merchant id.ID, role, path string) (crypto.WalletDerivation, crypto.MerchantWalletEnrollment, error) {
		require.Nil(t, postgres.MustGetTxManager(ctx).GetTx(ctx))
		address := "automatic-" + n.ID.String() + "-" + path
		network, err := crypto.MerchantCustodyNetwork(n)
		require.NoError(t, err)
		return crypto.WalletDerivation{Address: address, VaultKeyID: "synthetic-key-" + path, DerivationPath: path}, crypto.MerchantWalletEnrollment{SchemaVersion: "merchant_custody.v1", Network: network, Address: address, MerchantID: merchant.String(), Role: role}, nil
	})
}

func forcePoolReconcile(t *testing.T, f amlRoutingFixture, p crypto.MerchantPoolPolicy) {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_pool_policies SET next_reconcile_at=NOW() WHERE id=$1`, p.ID)
	require.NoError(t, err)
}

func TestAMLMerchantPoolConcurrentRefillAndPreflightDoNotMultiplyAddresses(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 2, 5)
	created := make([]int, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range created {
		wg.Go(func() { _, created[i], errs[i] = f.repo.ReconcileNextMerchantPool(f.ctx) })
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 2, created[0]+created[1])
	stats, err := f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Equal(t, 2, stats.Queued)
	forcePoolReconcile(t, f, p)
	_, count, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	for range 2 {
		claimed, _, err := service.RunNext(f.ctx, "control-worker")
		require.NoError(t, err)
		require.True(t, claimed)
	}
	claimed, _, err := service.RunNext(f.ctx, "another-worker")
	require.NoError(t, err)
	require.False(t, claimed)
	stats, err = f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Equal(t, crypto.MerchantPoolStats{Pending: 2, Total: 3, Started: 3}, stats)
	forcePoolReconcile(t, f, p)
	_, count, err = f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count, "unverified/unknown accounts occupy the pending target")
	var credits int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements`).Scan(&credits))
	require.Zero(t, credits)
}

func TestAMLMerchantPoolGlobalBudgetIncludesOtherMerchantsAndManualRequests(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 2, 5)
	other := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Second pool merchant')`, other)
	require.NoError(t, err)
	second := p
	second.ID = id.New()
	second.MerchantID = other
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version) SELECT $1,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$2 AND network_id=$3 AND token_id=$4`, other, p.MerchantID, p.NetworkID, f.input.Request.TokenID)
	require.NoError(t, err)
	_, err = f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: second, Actor: "operator"})
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET max_total_wallets=4 WHERE network_id=$1`, p.NetworkID)
	require.NoError(t, err)
	sum := 0
	for range 2 {
		_, count, err := f.repo.ReconcileNextMerchantPool(f.ctx)
		require.NoError(t, err)
		sum += count
	}
	require.Equal(t, 3, sum)
	_, err = f.repo.ClaimMerchantWalletProvision(f.ctx, crypto.MerchantWalletRequest{ID: id.New(), MerchantID: p.MerchantID, NetworkID: p.NetworkID, Role: "network_fee", Actor: "manual operator"}, "manual-worker", time.Minute)
	require.ErrorContains(t, err, "capacity exhausted")
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	claimed, _, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.True(t, claimed)
	var total int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT fn_crypto_aml_network_wallet_commitments($1,NULL)`, p.NetworkID).Scan(&total))
	require.Equal(t, 4, total)
}

func TestAMLMerchantPoolDisableCancelAndShrinkPreserveIntentHistory(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 2, 5)
	_, _, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	p.Enabled = false
	p, err = f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: p, ExpectedVersion: p.Version, Actor: "operator"})
	require.NoError(t, err)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	claimed, _, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.False(t, claimed)
	var cancelled id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM reg_crypto_aml_pool_intents WHERE policy_id=$1 ORDER BY id LIMIT 1`, p.ID).Scan(&cancelled))
	require.NoError(t, f.repo.CancelMerchantPoolIntent(f.ctx, cancelled, "target reduced before derivation", "operator"))
	_, err = f.repo.ClaimMerchantWalletProvision(f.ctx, crypto.MerchantWalletRequest{ID: cancelled, MerchantID: p.MerchantID, NetworkID: p.NetworkID, Role: p.Role, Actor: crypto.MerchantPoolActor}, "bypass-worker", time.Minute)
	require.ErrorContains(t, err, "cancelled")
	p.Enabled = true
	p.TargetReady = 1
	_, err = f.repo.ConfigureMerchantPoolPolicy(f.ctx, crypto.MerchantPoolPolicyRequest{Policy: p, ExpectedVersion: p.Version, Actor: "operator"})
	require.NoError(t, err)
	claimed, _, err = service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, _, err = service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.False(t, claimed)
}

func TestAMLMerchantPoolLostAckRetainsPathAndDoesNotBlockOtherIntent(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 2, 5)
	_, _, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	good := automaticPoolVault(t)
	calls := 0
	firstPath := ""
	vault := merchantProvisionStub(func(ctx context.Context, namespace id.ID, n crypto.NetworkRef, merchant id.ID, role, path string) (crypto.WalletDerivation, crypto.MerchantWalletEnrollment, error) {
		calls++
		derived, binding, err := good(ctx, namespace, n, merchant, role, path)
		if calls == 1 {
			firstPath = path
			return derived, binding, errors.New("synthetic enrollment ACK lost")
		}
		return derived, binding, err
	})
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, vault)
	claimed, _, err := service.RunNext(f.ctx, "first-worker")
	require.True(t, claimed)
	require.ErrorContains(t, err, "ACK lost")
	claimed, _, err = service.RunNext(f.ctx, "second-worker")
	require.True(t, claimed)
	require.NoError(t, err)
	var retryID id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM reg_crypto_aml_wallet_provisions WHERE status='retry_wait'`).Scan(&retryID))
	require.ErrorContains(t, f.repo.CancelMerchantPoolIntent(f.ctx, retryID, "cannot discard derived path", "operator"), "after derivation")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_wallet_provisions SET next_attempt_at=NOW() WHERE id=$1`, retryID)
	require.NoError(t, err)
	claimed, _, err = service.RunNext(f.ctx, "retry-worker")
	require.True(t, claimed)
	require.NoError(t, err)
	var persisted string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT derivation_path FROM reg_crypto_aml_wallet_provisions WHERE id=$1`, retryID).Scan(&persisted))
	require.Equal(t, firstPath, persisted)
	forcePoolReconcile(t, f, p)
	_, count, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestAMLMerchantPoolOpensOnlyPreflightedCollectorAndRotatesClosedBatch(t *testing.T) {
	f, p := automaticPoolFixture(t, "liquidity_hot", 1, 3)
	_, count, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	claimed, walletID, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.True(t, claimed)
	opened, err := f.repo.OpenNextReadyMerchantPoolBatch(f.ctx)
	require.NoError(t, err)
	require.False(t, opened)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET chain_activation_status='active' WHERE id=$1`, walletID)
	require.NoError(t, err)
	opened, err = f.repo.OpenNextReadyMerchantPoolBatch(f.ctx)
	require.NoError(t, err)
	require.False(t, opened)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_at=NOW(),resolved_by='integration-test',resolution='synthetic independent empty reconciliation' WHERE wallet_id=$1`, walletID)
	require.NoError(t, err)
	opened, err = f.repo.OpenNextReadyMerchantPoolBatch(f.ctx)
	require.NoError(t, err)
	require.False(t, opened, "no invented baseline")
	f.baseline(t, walletID)
	f.rebuild(t)
	opened, err = f.repo.OpenNextReadyMerchantPoolBatch(f.ctx)
	require.NoError(t, err)
	require.True(t, opened)
	opened, err = f.repo.OpenNextReadyMerchantPoolBatch(f.ctx)
	require.NoError(t, err)
	require.False(t, opened)
	stats, err := f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Ready)
	require.Zero(t, stats.Pending)
	batch, err := f.repo.ReadAMLBatch(f.ctx, p.MerchantID, walletID)
	require.NoError(t, err)
	require.NoError(t, f.repo.CloseAMLBatch(f.ctx, p.MerchantID, batch.ID, batch.Revision, "rotate after controlled intake", "operator"))
	forcePoolReconcile(t, f, p)
	_, count, err = f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	claimed, next, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEqual(t, walletID, next)
	forcePoolReconcile(t, f, p)
	_, count, err = f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestAMLMerchantPoolConcurrentConsumersReleaseSQLBeforeVault(t *testing.T) {
	f, _ := automaticPoolFixture(t, "merchant_pool", 2, 5)
	_, _, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	good := automaticPoolVault(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var calls atomic.Int32
	vault := merchantProvisionStub(func(ctx context.Context, namespace id.ID, n crypto.NetworkRef, merchant id.ID, role, path string) (crypto.WalletDerivation, crypto.MerchantWalletEnrollment, error) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return crypto.WalletDerivation{}, crypto.MerchantWalletEnrollment{}, ctx.Err()
			}
		}
		return good(ctx, namespace, n, merchant, role, path)
	})
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, vault)
	type outcome struct {
		wallet id.ID
		err    error
	}
	done := make(chan outcome, 1)
	go func() { _, wallet, err := service.RunNext(f.ctx, "first-control"); done <- outcome{wallet, err} }()
	select {
	case <-entered:
	case result := <-done:
		require.NoError(t, result.err)
		t.Fatal("first consumer did not call Vault")
	case <-time.After(5 * time.Second):
		t.Fatal("first consumer stalled")
	}
	claimed, second, err := service.RunNext(f.ctx, "second-control")
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, _, err = service.RunNext(f.ctx, "third-control")
	require.NoError(t, err)
	require.False(t, claimed)
	releaseOnce.Do(func() { close(release) })
	first := <-done
	require.NoError(t, first.err)
	require.NotEqual(t, first.wallet, second)
	require.EqualValues(t, 2, calls.Load())
}

func TestAMLMerchantPoolWatcherGateStopsNewClaimsAndLifetimeBudgetSurvivesRoleDrift(t *testing.T) {
	f, p := automaticPoolFixture(t, "liquidity_hot", 1, 1)
	_, _, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET lease_blocked_reason='watcher_lag',lease_blocked_at=NOW() WHERE network_id=$1`, p.NetworkID)
	require.NoError(t, err)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	claimed, _, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET lease_blocked_reason='',lease_blocked_at=NULL WHERE network_id=$1`, p.NetworkID)
	require.NoError(t, err)
	claimed, wallet, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET wallet_role='treasury',is_active=FALSE WHERE id=$1`, wallet)
	require.NoError(t, err)
	stats, err := f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Total)
	require.Zero(t, stats.Pending)
	forcePoolReconcile(t, f, p)
	_, count, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count, "retirement cannot mint fresh lifetime capacity")
}

func TestAMLMerchantPoolFeeAddressIsNotDuplicatedWhileAwaitingActivation(t *testing.T) {
	f, p := automaticPoolFixture(t, "network_fee", 1, 2)
	_, count, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	claimed, wallet, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.True(t, claimed)
	forcePoolReconcile(t, f, p)
	_, count, err = f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET is_active=FALSE WHERE id=$1`, wallet)
	require.NoError(t, err)
	forcePoolReconcile(t, f, p)
	_, count, err = f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	claimed, next, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEqual(t, wallet, next)
	var active int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM cat_wallets WHERE custody_merchant_id=$1 AND network_id=$2 AND wallet_role='network_fee' AND is_active AND NOT deletion_mark`, p.MerchantID, p.NetworkID).Scan(&active))
	require.Equal(t, 1, active)
}

func TestAMLMerchantPoolDepositNeedsBaselineAndHighStopsNewIntake(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 1, 4)
	_, _, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	claimed, wallet, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET chain_activation_status='active' WHERE id=$1`, wallet)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_at=NOW(),resolved_by='integration-test',resolution='synthetic independent reconciliation' WHERE wallet_id=$1 AND reason='chain_balance_check_pending'`, wallet)
	require.NoError(t, err)
	stats, err := f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Zero(t, stats.Ready)
	require.Equal(t, 1, stats.Pending)
	released, err := f.repo.ReleaseNextMerchantPoolBaselineHold(f.ctx)
	require.NoError(t, err)
	require.False(t, released)
	f.baseline(t, wallet)
	f.rebuild(t)
	released, err = f.repo.ReleaseNextMerchantPoolBaselineHold(f.ctx)
	require.NoError(t, err)
	require.True(t, released)
	stats, err = f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Ready)
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, wallet).Scan(&address))
	proof := f.proof(address, "high-auto-deposit", 1)
	proofID, effect := f.save(t, proof)
	f.postPayment(t, proof, proofID, wallet)
	f.creditNet(t, proof.Event.TxHash)
	f.complete(t, effect, 9000, true)
	f.rebuild(t)
	var held bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND status='open' AND owner_type='aml_pool_risk')`, wallet).Scan(&held))
	require.True(t, held)
	stats, err = f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Zero(t, stats.Ready)
	require.Zero(t, stats.Pending)
	forcePoolReconcile(t, f, p)
	_, count, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestAMLMerchantPoolNewAssetScopeReholdsDepositUntilIndependentBaseline(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 1, 4)
	_, _, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	_, wallet, err := service.RunNext(f.ctx, "control-worker")
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET chain_activation_status='active' WHERE id=$1`, wallet)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_at=NOW(),resolved_by='integration-test',resolution='synthetic independent reconciliation' WHERE wallet_id=$1 AND reason='chain_balance_check_pending'`, wallet)
	require.NoError(t, err)
	f.baseline(t, wallet)
	f.rebuild(t)
	released, err := f.repo.ReleaseNextMerchantPoolBaselineHold(f.ctx)
	require.NoError(t, err)
	require.True(t, released)
	asset := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard) VALUES($1,$1::uuid::text,'Second asset',$2,'second-contract','SECOND',6,'trc20')`, asset, p.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_provider_mappings(network_id,token_id,version,contract_version,contract_address,currency,provider_token_id,native,decimal_places,capability_accepted,acceptance_reference,currencylist_snapshot,pending_statuses) VALUES($1,$2,'second-asset-v1','synthetic-v1','second-contract','TRX','synthetic-second',FALSE,6,TRUE,'synthetic fixture','{"synthetic":true}',ARRAY['PENDING'])`, p.NetworkID, asset)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version) VALUES($1,$2,$3,TRUE,'routing-v1',$4,'second-asset-v1')`, p.MerchantID, p.NetworkID, asset, f.input.ProviderAccountID)
	require.NoError(t, err)
	stats, err := f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Zero(t, stats.Ready)
	released, err = f.repo.ReleaseNextMerchantPoolBaselineHold(f.ctx)
	require.NoError(t, err)
	require.False(t, released)
	other := f
	other.input.Request.TokenID = asset
	other.baseline(t, wallet)
	other.rebuild(t)
	released, err = f.repo.ReleaseNextMerchantPoolBaselineHold(f.ctx)
	require.NoError(t, err)
	require.True(t, released)
	stats, err = f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Ready)
}

func TestAMLMerchantPoolHistoryCannotBeRolledBack(t *testing.T) {
	f, _ := automaticPoolFixture(t, "merchant_pool", 1, 3)
	db, err := sql.Open("pgx", f.pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := dbassets.TenantMigrations()
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, migrationsBeforeAML)
	require.ErrorContains(t, err, "cannot roll back merchant pool history")
}

func TestAMLMerchantPoolActivationHoldRemainsPendingWithoutAddressChurn(t *testing.T) {
	f, p := automaticPoolFixture(t, "merchant_pool", 1, 4)
	_, count, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	claimed, _, err := service.RunNext(f.ctx, "control")
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET chain_activation_status='held' WHERE id IN (SELECT wallet_id FROM reg_crypto_aml_wallet_provisions WHERE merchant_id=$1 AND network_id=$2)`, p.MerchantID, p.NetworkID)
	require.NoError(t, err)
	stats, err := f.repo.ReadMerchantPoolStats(f.ctx, p.MerchantID, p.ID)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Pending)
	require.Zero(t, stats.Ready)
	forcePoolReconcile(t, f, p)
	_, count, err = f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count, "a technical activation failure needs recovery, not a fresh address")
}
