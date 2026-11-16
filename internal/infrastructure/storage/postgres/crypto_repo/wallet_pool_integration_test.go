//go:build integration

package crypto_repo

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_catalog_repo"
)

func TestIntegrationConcurrentWalletPoolControllersCreateOnlyDeficit(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	networkID, tokenID, merchantID := id.New(), id.New(), id.New()
	suffix := id.Compact(networkID)
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks (
		    id, code, name, chain_family, network_identifier, native_token_symbol, native_decimals
		) VALUES ($1, $2, 'Wallet pool concurrency test', 'tron', $3, 'TRX', 6)
	`, networkID, "TRON-NILE-POOL-"+suffix, "pool-"+suffix)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM reg_merchant_wallet_lease_rate_windows WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM reg_merchant_wallet_allocation_overrides WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_wallet_provisioning_jobs WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_wallet_derivation_cursors WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_wallet_pool_policies WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_wallet_derivations WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM reg_crypto_wallet_allocations WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_wallets WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_tokens WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_network_runtime_status WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_external_resource_policies WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_network_policies WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_blockchain_networks WHERE id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_merchants WHERE id = $1`, merchantID)
	})
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_merchants (id, code, name)
		VALUES ($1, $2, 'Wallet pool concurrency merchant')
	`, merchantID, "POOL-MERCHANT-"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_tokens (
		    id, code, name, network_id, contract_address, symbol,
		    decimal_places, token_standard
		) VALUES ($1, $2, 'Native TRX', $3, '', 'TRX', 6, 'native')
	`, tokenID, "POOL-TRX-"+suffix, networkID)
	require.NoError(t, err)
	namespace := fmt.Sprintf("m/44'/195'/%d'/0", int(networkID[0])+1000)
	_, err = pool.Exec(ctx, `
		INSERT INTO sys_crypto_wallet_pool_policies (
		    network_id, enabled, low_watermark, target_ready, max_total_wallets,
		    max_provision_batch, max_concurrent_activations,
		    activation_reserve_minor, min_hot_native_balance_minor,
		    daily_activation_budget_minor, retry_after_seconds,
		    derivation_namespace, needs_review,
		    max_active_invoice_leases_per_merchant,
		    max_lease_starts_per_minute_per_merchant,
		    min_global_ready_reserve
		) VALUES (
		    $1, TRUE, 2, 5, 20,
		    5, 1,
		    2000000, 2000000,
		    10000000, 30,
		    $2, FALSE,
		    2, 3, 0
		)
		ON CONFLICT (network_id) DO UPDATE
		SET enabled = EXCLUDED.enabled,
		    low_watermark = EXCLUDED.low_watermark,
		    target_ready = EXCLUDED.target_ready,
		    max_total_wallets = EXCLUDED.max_total_wallets,
		    max_provision_batch = EXCLUDED.max_provision_batch,
		    max_concurrent_activations = EXCLUDED.max_concurrent_activations,
		    activation_reserve_minor = EXCLUDED.activation_reserve_minor,
		    min_hot_native_balance_minor = EXCLUDED.min_hot_native_balance_minor,
		    daily_activation_budget_minor = EXCLUDED.daily_activation_budget_minor,
		    retry_after_seconds = EXCLUDED.retry_after_seconds,
		    derivation_namespace = EXCLUDED.derivation_namespace,
		    needs_review = EXCLUDED.needs_review,
		    max_active_invoice_leases_per_merchant = EXCLUDED.max_active_invoice_leases_per_merchant,
		    max_lease_starts_per_minute_per_merchant = EXCLUDED.max_lease_starts_per_minute_per_merchant,
		    min_global_ready_reserve = EXCLUDED.min_global_ready_reserve
	`, networkID, namespace)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO sys_crypto_wallet_derivation_cursors (
		    network_id, derivation_namespace, next_index, needs_review
		) VALUES ($1, $2, 0, FALSE)
	`, networkID, namespace)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_wallets (
		    code, name, network_id, address, derivation_path, vault_key_id,
		    tier, status, allocation_mode, chain_activation_status
		) VALUES
		    ($1, 'Held A', $2, $3, $4, $5, 'pool', 'free', 'transient', 'held'),
		    ($6, 'Held B', $2, $7, $8, $9, 'pool', 'free', 'transient', 'held'),
		    ($10, 'Reconciliation pending', $2, $11, $12, $13,
		     'pool', 'free', 'transient', 'active');
		INSERT INTO reg_crypto_wallet_allocation_holds (
		    wallet_id, reason, owner_type, owner_id, details, held_by
		)
		SELECT id, 'chain_balance_check_pending', 'wallet_pool_bootstrap',
		       'wallet-pool-integration', '{}'::jsonb, 'integration-test'
		FROM cat_wallets
		WHERE code = $10
	`, pgx.QueryExecModeSimpleProtocol, "POOL-HELD-A-"+suffix, networkID, "THeldA"+suffix, namespace+"/90", "vault/held-a/"+suffix,
		"POOL-HELD-B-"+suffix, "THeldB"+suffix, namespace+"/91", "vault/held-b/"+suffix,
		"POOL-HELD-C-"+suffix, "THeldC"+suffix, namespace+"/92", "vault/held-c/"+suffix)
	require.NoError(t, err)

	repo := NewWalletPoolRepo()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, planErr := repo.PlanNetworkRefill(ctx, networkID)
			errs <- planErr
		}()
	}
	wg.Wait()
	close(errs)
	for planErr := range errs {
		require.NoError(t, planErr)
	}

	var count int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM sys_crypto_wallet_provisioning_jobs
		WHERE network_id = $1
	`, networkID).Scan(&count))
	require.Equal(t, 2, count, "activation and reconciliation holds must count as in-flight capacity")

	claimedA, err := repo.ClaimProvisioningJobs(ctx, "worker-a", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimedA, 1)
	jobA := claimedA[0]
	_, err = pool.Exec(ctx, `
		UPDATE sys_crypto_wallet_provisioning_jobs
		SET status = 'held'
		WHERE network_id = $1
		  AND id <> $2
	`, networkID, jobA.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE sys_crypto_wallet_provisioning_jobs
		SET lease_expires_at = NOW() - interval '1 second'
		WHERE id = $1
	`, jobA.ID)
	require.NoError(t, err)

	claimedB, err := repo.ClaimProvisioningJobs(ctx, "worker-b", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimedB, 1)
	jobB := claimedB[0]
	require.Equal(t, jobA.ID, jobB.ID)
	require.NotEqual(t, jobA.ClaimToken, jobB.ClaimToken)

	derivation := crypto.WalletDerivation{
		Address:        "TProvisioned" + suffix,
		VaultKeyID:     "vault/provisioned/" + suffix,
		DerivationPath: jobB.DerivationPath,
	}
	_, err = repo.CompleteProvisioningJob(ctx, jobA, derivation, crypto.WalletProvisioningDerivedWithActivation)
	require.ErrorContains(t, err, "fencing token is stale")

	provisionedWalletID, err := repo.CompleteProvisioningJob(ctx, jobB, derivation, crypto.WalletProvisioningDerivedWithActivation)
	require.NoError(t, err)
	idempotentWalletID, err := repo.CompleteProvisioningJob(ctx, jobB, derivation, crypto.WalletProvisioningDerivedWithActivation)
	require.NoError(t, err)
	require.Equal(t, provisionedWalletID, idempotentWalletID)

	_, err = pool.Exec(ctx, `
		INSERT INTO cat_wallets (
		    code, name, network_id, address, derivation_path, vault_key_id,
		    tier, status, allocation_mode, chain_activation_status
		) VALUES (
		    $1, 'Duplicate path', $2, $3, $4, $5,
		    'pool', 'free', 'transient', 'unknown'
		)
	`, "POOL-DUPLICATE-"+suffix, networkID, "TDuplicate"+suffix,
		jobB.DerivationPath, "vault/duplicate/"+suffix)
	require.Error(t, err, "a published derivation path must never bind to a second wallet")

	_, err = pool.Exec(ctx, `
		INSERT INTO cat_wallets (
		    code, name, network_id, address, derivation_path, vault_key_id,
		    tier, status, allocation_mode, chain_activation_status
		) VALUES
		    ($1, 'Lease A', $2, $3, $4, $5, 'pool', 'free', 'transient', 'active'),
		    ($6, 'Lease B', $2, $7, $8, $9, 'pool', 'free', 'transient', 'active')
	`, "POOL-LEASE-A-"+suffix, networkID, "TLeaseA"+suffix, namespace+"/100", "vault/lease-a/"+suffix,
		"POOL-LEASE-B-"+suffix, "TLeaseB"+suffix, namespace+"/101", "vault/lease-b/"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE sys_crypto_network_policies
		SET enabled=TRUE,
		    per_transaction_max_fee_minor=1,
		    daily_network_fee_budget_minor=1000,
		    fee_funding_mode='source_only'
		WHERE network_id=$1
	`, networkID)
	require.NoError(t, err)

	walletRepo := crypto_catalog_repo.NewWalletRepo()
	expiresAt := time.Now().UTC().Add(47 * time.Minute).Truncate(time.Microsecond)
	type leaseResult struct {
		walletID    id.ID
		leasedUntil *time.Time
		err         error
	}
	leaseResults := make(chan leaseResult, 3)
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			leased, leaseErr := walletRepo.LeaseForInvoice(ctx, merchantID, id.New(), networkID, tokenID, expiresAt)
			if leaseErr != nil {
				leaseResults <- leaseResult{err: leaseErr}
				return
			}
			leaseResults <- leaseResult{walletID: leased.ID, leasedUntil: leased.LeasedUntil}
		}()
	}
	wg.Wait()
	close(leaseResults)

	leasedIDs := make(map[id.ID]struct{})
	var exhausted int
	for result := range leaseResults {
		if result.err != nil {
			appErr, ok := apperror.AsAppError(result.err)
			require.True(t, ok)
			require.Equal(t, wallet.CodeWalletPoolExhausted, appErr.Code)
			require.Equal(t, 503, appErr.HTTPStatus)
			exhausted++
			continue
		}
		require.NotNil(t, result.leasedUntil)
		require.True(t, result.leasedUntil.Equal(expiresAt))
		leasedIDs[result.walletID] = struct{}{}
	}
	require.Len(t, leasedIDs, 2, "one ready address must never be leased twice")
	require.Equal(t, 1, exhausted)
}
