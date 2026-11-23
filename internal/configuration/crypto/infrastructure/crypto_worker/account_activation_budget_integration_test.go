//go:build integration

package crypto_worker

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

func TestIntegrationAccountActivationBudgetReservationUsesLedgerAndConcurrency(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	networkID, tokenID := id.New(), id.New()
	hotWalletID, poolWalletA, poolWalletB := id.New(), id.New(), id.New()
	activationA, activationB := id.New(), id.New()
	suffix := networkID.String()
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks (
		    id, code, name, chain_family, network_identifier, native_token_symbol, native_decimals
		) VALUES ($1, $2, 'Activation budget test', 'tron', $3, 'TRX', 6)
	`, networkID, "BUDGET-"+suffix, "budget-"+suffix)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM reg_crypto_account_activations WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM reg_crypto_balance_balances WHERE token_id = $1`, tokenID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_wallet_pool_policies WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_wallet_derivations WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_wallets WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_tokens WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_network_runtime_status WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_external_resource_policies WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_network_policies WHERE network_id = $1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_blockchain_networks WHERE id = $1`, networkID)
	})
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_tokens (
		    id, code, name, network_id, contract_address, symbol,
		    decimal_places, token_standard
		) VALUES ($1, $2, 'Native TRX', $3, '', 'TRX', 6, 'native')
	`, tokenID, "BUDGET-TRX-"+suffix, networkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_wallets (
		    id, code, name, network_id, address, derivation_path, vault_key_id,
		    tier, status, chain_activation_status, wallet_role
		) VALUES
		    ($1, $2, 'Hot', $3, $4, $5, $6, 'hot', 'free', 'active', 'liquidity_hot'),
		    ($7, $8, 'Pool A', $3, $9, $10, $11, 'pool', 'free', 'activating', 'merchant_pool'),
		    ($12, $13, 'Pool B', $3, $14, $15, $16, 'pool', 'free', 'activating', 'merchant_pool')
	`, hotWalletID, "BUDGET-HOT-"+suffix, networkID,
		"THot"+suffix, "m/44'/195'/900'/0/0", "vault/hot/"+suffix,
		poolWalletA, "BUDGET-POOL-A-"+suffix, "TPoolA"+suffix, "m/44'/195'/900'/0/1", "vault/pool-a/"+suffix,
		poolWalletB, "BUDGET-POOL-B-"+suffix, "TPoolB"+suffix, "m/44'/195'/900'/0/2", "vault/pool-b/"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO sys_crypto_wallet_pool_policies (
		    network_id, enabled, low_watermark, target_ready, max_total_wallets,
		    max_provision_batch, max_concurrent_activations,
		    activation_reserve_minor, min_hot_native_balance_minor,
		    daily_activation_budget_minor, retry_after_seconds,
		    derivation_namespace, needs_review
		) VALUES (
		    $1, TRUE, 1, 2, 10,
		    2, 1,
		    2000000, 2000000,
		    10000000, 30,
		    $2, FALSE
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
		    needs_review = EXCLUDED.needs_review
	`, networkID, "m/44'/195'/900'/0")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO reg_crypto_balance_balances (wallet_id, token_id, amount)
		VALUES ($1, $2, 6000000)
	`, hotWalletID, tokenID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO reg_crypto_account_activations (
		    id, wallet_id, network_id, status
		) VALUES
		    ($1, $2, $3, 'claimed'),
		    ($4, $5, $3, 'claimed')
	`, activationA, poolWalletA, networkID, activationB, poolWalletB)
	require.NoError(t, err)

	repo := NewOutboundRepo()
	_, err = pool.Exec(ctx, `
		UPDATE sys_crypto_wallet_pool_policies
		SET enabled = FALSE
		WHERE network_id = $1
	`, networkID)
	require.NoError(t, err)
	claimedTasks, err := repo.ClaimAccountActivationTasks(ctx, "disabled-policy-worker", 2)
	require.NoError(t, err)
	require.Empty(t, claimedTasks)

	var attemptsWhileDisabled int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT SUM(activation_attempts)
		FROM reg_crypto_account_activations
		WHERE network_id = $1
	`, networkID).Scan(&attemptsWhileDisabled))
	require.Zero(t, attemptsWhileDisabled)

	_, err = pool.Exec(ctx, `
		UPDATE sys_crypto_wallet_pool_policies
		SET enabled = TRUE
		WHERE network_id = $1
	`, networkID)
	require.NoError(t, err)
	claimedTasks, err = repo.ClaimAccountActivationTasks(ctx, "budget-test-worker", 2)
	require.NoError(t, err)
	require.Len(t, claimedTasks, 2)
	tasksByID := make(map[id.ID]AccountActivationTask, len(claimedTasks))
	for _, task := range claimedTasks {
		tasksByID[task.ActivationID] = task
	}
	taskA, ok := tasksByID[activationA]
	require.True(t, ok)
	taskB, ok := tasksByID[activationB]
	require.True(t, ok)

	require.NoError(t, repo.ReserveAccountActivationBudget(ctx, taskA))
	// Idempotent retry of the same activation does not double-reserve.
	require.NoError(t, repo.ReserveAccountActivationBudget(ctx, taskA))

	err = repo.ReserveAccountActivationBudget(ctx, taskB)
	require.ErrorContains(t, err, "concurrency limit")
	require.ErrorIs(t, err, outboundapp.ErrActivationConcurrencyLimit)
	require.NoError(t, repo.ApplyAccountActivationClaim(ctx, taskB, outboundapp.ActivationClaimUpdate{Action: outboundapp.ActivationClaimDeferred, Delay: _accountActivationPeriod}))

	var claimReleased, retryScheduled bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT activation_claim_token IS NULL
		       AND activation_lease_expires_at IS NULL,
		       activation_next_attempt_at > NOW()
		FROM reg_crypto_account_activations
		WHERE id = $1
	`, taskB.ActivationID).Scan(&claimReleased, &retryScheduled))
	require.True(t, claimReleased)
	require.True(t, retryScheduled)

	var reserved int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT SUM(activation_cost_reserved_minor)
		FROM reg_crypto_account_activations
		WHERE network_id = $1
	`, networkID).Scan(&reserved))
	require.EqualValues(t, 2_000_000, reserved)

	// Reservation presence, not a positive amount, owns the concurrency slot.
	// This protects fee-free policies and prevents zero from bypassing the gate.
	_, err = pool.Exec(ctx, `
		UPDATE sys_crypto_wallet_pool_policies
		SET activation_reserve_minor = 0
		WHERE network_id = $1
	`, networkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE reg_crypto_account_activations
		SET status = 'claimed',
		    activation_cost_reserved_minor = 0,
		    activation_cost_reserved_at = NULL,activation_claim_token=NULL,activation_lease_expires_at=NULL,activation_next_attempt_at=clock_timestamp()-interval '1 second'
		WHERE network_id = $1
	`, networkID)
	require.NoError(t, err)

	claimedTasks, err = repo.ClaimAccountActivationTasks(ctx, "budget-second-round", 2)
	require.NoError(t, err)
	require.Len(t, claimedTasks, 2)
	for _, task := range claimedTasks {
		switch task.ActivationID {
		case activationA:
			taskA = task
		case activationB:
			taskB = task
		}
	}

	require.NoError(t, repo.ReserveAccountActivationBudget(ctx, taskA))
	err = repo.ReserveAccountActivationBudget(ctx, taskB)
	require.ErrorIs(t, err, outboundapp.ErrActivationConcurrencyLimit)

	var reservations int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM reg_crypto_account_activations
		WHERE network_id = $1
		  AND activation_cost_reserved_at IS NOT NULL
	`, networkID).Scan(&reservations))
	require.Equal(t, 1, reservations)

	// Pre-derived wallets must not all be activated merely because they are
	// present. The activation queue is bounded by the policy's ready target.
	_, err = pool.Exec(ctx, `
		UPDATE reg_crypto_account_activations SET status='failed',activation_cost_reserved_minor=0,activation_cost_reserved_at=NULL,activation_claim_token=NULL,activation_lease_expires_at=NULL
		WHERE network_id = $1
	`, networkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE cat_wallets
		SET chain_activation_status = 'unknown'
		WHERE id IN ($1, $2)
	`, poolWalletA, poolWalletB)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE sys_crypto_wallet_pool_policies
		SET low_watermark = 0,
		    target_ready = 1,
		    max_total_wallets = 2
		WHERE network_id = $1
	`, networkID)
	require.NoError(t, err)
	var candidateWallets, hotWallets, nativeTokens int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cat_wallets
		WHERE network_id = $1
		  AND tier = 'pool'
		  AND wallet_role = 'merchant_pool'
		  AND is_active = TRUE
		  AND deletion_mark = FALSE
		  AND address <> ''
		  AND vault_key_id <> ''
		  AND chain_activation_status = 'unknown'
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_wallet_allocation_holds h
		      WHERE h.wallet_id = cat_wallets.id AND h.status = 'open'
		  )
	`, networkID).Scan(&candidateWallets))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cat_wallets
		WHERE network_id = $1
		  AND tier = 'hot'
		  AND wallet_role = 'liquidity_hot'
		  AND is_active = TRUE
		  AND deletion_mark = FALSE
		  AND address <> ''
		  AND vault_key_id <> ''
	`, networkID).Scan(&hotWallets))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cat_tokens
		WHERE network_id = $1
		  AND token_standard = 'native'
		  AND is_active = TRUE
		  AND deletion_mark = FALSE
	`, networkID).Scan(&nativeTokens))
	require.Equal(t, 2, candidateWallets)
	require.Equal(t, 1, hotWallets)
	require.Equal(t, 1, nativeTokens)
	claimedTasks, err = repo.ClaimAccountActivationTasks(ctx, "target-ready-worker", 8)
	require.NoError(t, err)
	require.Len(t, claimedTasks, 1)

	var queued, activating, unknown int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM reg_crypto_account_activations WHERE network_id = $1 AND status IN ('claimed','signing','broadcast','confirmed','held')),
		       COUNT(*) FILTER (WHERE chain_activation_status = 'activating')::INT,
		       COUNT(*) FILTER (WHERE chain_activation_status = 'unknown')::INT
		FROM cat_wallets
		WHERE network_id = $1
		  AND tier = 'pool'
	`, networkID).Scan(&queued, &activating, &unknown))
	require.Equal(t, 1, queued)
	require.Equal(t, 1, activating)
	require.Equal(t, 1, unknown)
}
