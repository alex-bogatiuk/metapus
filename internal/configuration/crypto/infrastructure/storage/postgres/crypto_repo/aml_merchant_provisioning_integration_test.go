//go:build integration

package crypto_repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"

	"github.com/stretchr/testify/require"
)

type merchantProvisionStub func(context.Context, id.ID, crypto.NetworkRef, id.ID, string, string) (crypto.WalletDerivation, crypto.MerchantWalletEnrollment, error)

func (s merchantProvisionStub) DeriveAndEnroll(ctx context.Context, namespace id.ID, network crypto.NetworkRef, merchant id.ID, role, path string) (crypto.WalletDerivation, crypto.MerchantWalletEnrollment, error) {
	return s(ctx, namespace, network, merchant, role, path)
}

func merchantProvisionFixture(t *testing.T, family ...crypto.ChainFamily) (amlRoutingFixture, crypto.MerchantWalletRequest, id.ID) {
	t.Helper()
	f := newAMLRoutingFixture(t, false, family...)
	namespace := id.New()
	_, err := f.pool.Exec(f.ctx, `UPDATE cat_blockchain_networks SET network_identifier='nile' WHERE id=$1 AND chain_family='tron'`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET max_total_wallets=100,derivation_namespace='m/44''/195''/0''/0',needs_review=FALSE WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_wallet_derivation_cursors(network_id,derivation_namespace,next_index,needs_review) VALUES($1,'m/44''/195''/0''/0',100,FALSE) ON CONFLICT(network_id) DO UPDATE SET derivation_namespace=EXCLUDED.derivation_namespace,next_index=100,needs_review=FALSE`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_provisioning_identity(vault_namespace,database_name,enrolled_by) VALUES($1,current_database(),'integration-test')`, namespace)
	require.NoError(t, err)
	return f, crypto.MerchantWalletRequest{ID: id.New(), MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID, Role: "liquidity_hot", Actor: "integration-test"}, namespace
}

func syntheticMerchantEnrollment(request crypto.MerchantWalletRequest, path string) (crypto.WalletDerivation, crypto.MerchantWalletEnrollment) {
	address := "provision-" + request.ID.String()
	return crypto.WalletDerivation{Address: address, VaultKeyID: "synthetic-key", DerivationPath: path}, crypto.MerchantWalletEnrollment{SchemaVersion: "merchant_custody.v1", Network: "tron_nile", Address: address, MerchantID: request.MerchantID.String(), Role: request.Role}
}

func TestAMLMerchantProvisionEnrollsBeforePublicationAndReplays(t *testing.T) {
	f, request, namespace := merchantProvisionFixture(t)
	calls := 0
	vault := merchantProvisionStub(func(ctx context.Context, actual id.ID, _ crypto.NetworkRef, _ id.ID, _, path string) (crypto.WalletDerivation, crypto.MerchantWalletEnrollment, error) {
		calls++
		require.Equal(t, namespace, actual)
		require.Nil(t, postgres.MustGetTxManager(ctx).GetTx(ctx), "external Vault call cannot inherit a SQL transaction")
		var exists bool
		require.NoError(t, f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_wallets WHERE id=$1)`, request.ID).Scan(&exists))
		require.False(t, exists)
		derived, binding := syntheticMerchantEnrollment(request, path)
		return derived, binding, nil
	})
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, vault)
	wallet, err := service.Run(f.ctx, request, "control-worker")
	require.NoError(t, err)
	require.Equal(t, request.ID, wallet)
	again, err := service.Run(f.ctx, request, "control-worker")
	require.NoError(t, err)
	require.Equal(t, wallet, again)
	require.Equal(t, 1, calls)
	var owner id.ID
	var role, activation string
	var held, credits int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT custody_merchant_id,wallet_role,chain_activation_status,(SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=w.id AND status='open'),(SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements) FROM cat_wallets w WHERE id=$1`, wallet).Scan(&owner, &role, &activation, &held, &credits))
	require.Equal(t, request.MerchantID, owner)
	require.Equal(t, "liquidity_hot", role)
	require.Equal(t, "unknown", activation)
	require.Equal(t, 1, held)
	require.Zero(t, credits)
	request.Role = "merchant_pool"
	_, err = service.Run(f.ctx, request, "control-worker")
	require.ErrorContains(t, err, "identity changed")
}

func TestAMLMerchantProvisionLostEnrollmentAckKeepsSamePath(t *testing.T) {
	f, request, _ := merchantProvisionFixture(t)
	var firstPath string
	calls := 0
	vault := merchantProvisionStub(func(_ context.Context, _ id.ID, _ crypto.NetworkRef, _ id.ID, _, path string) (crypto.WalletDerivation, crypto.MerchantWalletEnrollment, error) {
		calls++
		derived, binding := syntheticMerchantEnrollment(request, path)
		if calls == 1 {
			firstPath = path
			return derived, binding, errors.New("synthetic lost enrollment ACK")
		}
		require.Equal(t, firstPath, path)
		return derived, binding, nil
	})
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, vault)
	_, err := service.Run(f.ctx, request, "first-worker")
	require.ErrorContains(t, err, "lost enrollment ACK")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM cat_wallets WHERE id=$1`, request.ID).Scan(&count))
	require.Zero(t, count)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_wallet_provisions SET next_attempt_at=NOW() WHERE id=$1`, request.ID)
	require.NoError(t, err)
	_, err = service.Run(f.ctx, request, "second-worker")
	require.NoError(t, err)
	var paths int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_crypto_wallet_derivations WHERE network_id=$1 AND derivation_path=$2`, request.NetworkID, firstPath).Scan(&paths))
	require.Equal(t, 1, paths)
}

func TestAMLMerchantProvisionFencesExpiredLeaseAndWrongBinding(t *testing.T) {
	f, request, _ := merchantProvisionFixture(t)
	old, err := f.repo.ClaimMerchantWalletProvision(f.ctx, request, "old-worker", time.Minute)
	require.NoError(t, err)
	_, err = f.repo.ClaimMerchantWalletProvision(f.ctx, request, "new-worker", time.Minute)
	require.ErrorContains(t, err, "leased")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_wallet_provisions SET lease_expires_at=NOW()-interval '1 second' WHERE id=$1`, request.ID)
	require.NoError(t, err)
	current, err := f.repo.ClaimMerchantWalletProvision(f.ctx, request, "new-worker", time.Minute)
	require.NoError(t, err)
	require.Equal(t, old.Job.DerivationPath, current.Job.DerivationPath)
	derived, binding := syntheticMerchantEnrollment(request, old.Job.DerivationPath)
	_, err = f.repo.CompleteMerchantWalletProvision(f.ctx, old, derived, binding)
	require.ErrorContains(t, err, "lease or identity changed")
	wrong := binding
	wrong.MerchantID = id.New().String()
	_, err = f.repo.CompleteMerchantWalletProvision(f.ctx, current, derived, wrong)
	require.ErrorContains(t, err, "proof mismatch")
	_, err = f.repo.CompleteMerchantWalletProvision(f.ctx, current, derived, binding)
	require.NoError(t, err)
}

func TestAMLMerchantProvisionRejectsOuterTransactionAndPausesSharedPool(t *testing.T) {
	f, request, _ := merchantProvisionFixture(t)
	err := postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		_, err := f.repo.ClaimMerchantWalletProvision(ctx, request, "control-worker", time.Minute)
		return err
	})
	require.ErrorContains(t, err, "inside a SQL transaction")
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET enabled=TRUE WHERE network_id=$1`, request.NetworkID)
	require.NoError(t, err)
	repo := NewWalletPoolRepo()
	candidates, err := repo.ListRefillCandidates(f.ctx)
	require.NoError(t, err)
	require.NotContains(t, candidates, request.NetworkID)
	plan, err := repo.PlanNetworkRefill(f.ctx, request.NetworkID)
	require.NoError(t, err)
	require.Zero(t, plan.Created)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_wallet_derivations(network_id,derivation_path,source) VALUES($1,'m/44''/195''/0''/0/99','provisioning')`, request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_wallet_provisioning_jobs(network_id,derivation_path) VALUES($1,'m/44''/195''/0''/0/99')`, request.NetworkID)
	require.NoError(t, err)
	jobs, err := repo.ClaimProvisioningJobs(f.ctx, "ordinary-worker", 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, jobs)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(code,name,network_id,address,vault_key_id,derivation_path) VALUES('blocked-shared','Blocked shared',$1,'blocked-shared-address','synthetic','m/44''/195''/0''/0/99')`, request.NetworkID)
	require.ErrorContains(t, err, "trusted merchant provisioning")
}
