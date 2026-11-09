//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestClaimRunnableSweepAttemptsResumesPreSignHoldAfterPolicyUpdate(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	errRollback := errors.New("rollback integration fixture")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		networkID, nativeTokenID, tokenID := id.New(), id.New(), id.New()
		hotWalletID, poolWalletID := id.New(), id.New()
		sweepID, attemptID := id.New(), id.New()
		suffix := id.Compact(networkID)

		_, err := q.Exec(txCtx, `
			INSERT INTO cat_blockchain_networks (
				id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals
			) VALUES ($1,$2,'Policy recovery network','tron',$3,'TRX',6)
		`, networkID, "RECOVERY-"+suffix, "recovery-"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO cat_tokens (
				id,code,name,network_id,symbol,decimal_places,token_standard,contract_address
			) VALUES
				($1,$3,'Recovery TRX',$5,'TRX',6,'native',''),
				($2,$4,'Recovery USDT',$5,'USDT',6,'TRC-20',$6)
		`, nativeTokenID, tokenID, "RECOVERY-TRX-"+suffix, "RECOVERY-USDT-"+suffix,
			networkID, "TContract"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO cat_wallets (
				id,code,name,network_id,address,tier,wallet_role,status,vault_key_id,chain_activation_status
			) VALUES
				($1,$3,'Recovery hot',$5,$6,'hot','liquidity_hot','free',$8,'active'),
				($2,$4,'Recovery pool',$5,$7,'pool','merchant_pool','sweeping',$9,'active')
		`, hotWalletID, poolWalletID, "RECOVERY-HOT-"+suffix, "RECOVERY-POOL-"+suffix,
			networkID, "THot"+suffix, "TPool"+suffix, "vault/hot/"+suffix, "vault/pool/"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO doc_crypto_sweeps (accounting_model_version,
				id,number,token_id,hot_wallet_id,total_amount,status
			) VALUES (1,$1,$2,$3,$4,100000,'held')
		`, sweepID, "CS-RECOVERY-"+suffix, tokenID, hotWalletID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO reg_crypto_sweep_attempts (
				id,wallet_id,token_id,network_id,sweep_id,hot_wallet_id,
				source_address,destination_address,amount,status,error_message,expires_at,updated_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,$7,$8,100000,'held','fee cap exceeded',
				NOW()+interval '10 minutes',NOW()-interval '1 minute'
			)
		`, attemptID, poolWalletID, tokenID, networkID, sweepID, hotWalletID,
			"TPool"+suffix, "THot"+suffix)
		require.NoError(t, err)

		repo := NewOutboundRepo()
		before, err := repo.ClaimRunnableSweepAttempts(txCtx, 100)
		require.NoError(t, err)
		require.False(t, containsSweepAttempt(before, attemptID))

		_, err = q.Exec(txCtx, `
			UPDATE sys_crypto_network_policies
			SET enabled=TRUE,
				per_transaction_max_fee_minor=100000000,
				daily_network_fee_budget_minor=100000000,
				fee_funding_mode='jit',
				daily_jit_funding_budget_minor=200000000,
				jit_funding_max_amount_minor=101000000,
				version=version+1,
				updated_at=NOW()+interval '1 second'
			WHERE network_id=$1
		`, networkID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO sys_crypto_external_resource_policies
				(network_id,resource_strategy,provider_account_ids,provider_failure_threshold,provider_open_seconds)
			VALUES($1,'burn_only',ARRAY[]::uuid[],3,60)
			ON CONFLICT (network_id) DO UPDATE SET resource_strategy=EXCLUDED.resource_strategy,
				provider_account_ids=EXCLUDED.provider_account_ids,provider_failure_threshold=EXCLUDED.provider_failure_threshold,
				provider_open_seconds=EXCLUDED.provider_open_seconds,updated_at=NOW()
		`, networkID)
		require.NoError(t, err)

		after, err := repo.ClaimRunnableSweepAttempts(txCtx, 100)
		require.NoError(t, err)
		require.True(t, containsSweepAttempt(after, attemptID))

		var status string
		require.NoError(t, q.QueryRow(txCtx, `SELECT status FROM reg_crypto_sweep_attempts WHERE id=$1`, attemptID).Scan(&status))
		require.Equal(t, "claimed", status)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}

func TestClaimRunnableSweepAttemptsResumesPendingResourceRental(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	errRollback := errors.New("rollback integration fixture")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		networkID, nativeTokenID, tokenID := id.New(), id.New(), id.New()
		hotWalletID, poolWalletID, expiredPoolWalletID := id.New(), id.New(), id.New()
		providerAccountID := id.New()
		sweepID, attemptID, rentalID := id.New(), id.New(), id.New()
		expiredSweepID, expiredAttemptID, expiredRentalID := id.New(), id.New(), id.New()
		suffix := id.Compact(networkID)

		_, err := q.Exec(txCtx, `
			INSERT INTO cat_blockchain_networks (
				id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals
			) VALUES ($1,$2,'Rental recovery network','tron',$3,'TRX',6)
		`, networkID, "RENTAL-RECOVERY-"+suffix, "rental-recovery-"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO cat_tokens (
				id,code,name,network_id,symbol,decimal_places,token_standard,contract_address
			) VALUES
				($1,$3,'Rental recovery TRX',$5,'TRX',6,'native',''),
				($2,$4,'Rental recovery USDT',$5,'USDT',6,'TRC-20',$6)
		`, nativeTokenID, tokenID, "RR-TRX-"+suffix,
			"RR-USDT-"+suffix, networkID, "TContract"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO cat_wallets (
				id,code,name,network_id,address,tier,wallet_role,status,vault_key_id,chain_activation_status
			) VALUES
				($1,$3,'Rental recovery hot',$5,$6,'hot','liquidity_hot','free',$8,'active'),
				($2,$4,'Rental recovery pool',$5,$7,'pool','merchant_pool','sweeping',$9,'active'),
				($10,$11,'Expired rental recovery pool',$5,$12,'pool','merchant_pool','sweeping',$13,'active')
		`, hotWalletID, poolWalletID, "RR-HOT-"+suffix,
			"RR-POOL-"+suffix, networkID, "THot"+suffix, "TPool"+suffix,
			"vault/hot/"+suffix, "vault/pool/"+suffix, expiredPoolWalletID,
			"RR-EXPIRED-POOL-"+suffix, "TExpiredPool"+suffix, "vault/expired-pool/"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO cat_crypto_provider_accounts (
				id,code,name,provider,network_id,balance_token_id,is_active
			) VALUES ($1,$2,'Rental recovery provider','itrx',$3,$4,TRUE)
		`, providerAccountID, "RR-PROVIDER-"+suffix, networkID, nativeTokenID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO doc_crypto_sweeps (accounting_model_version,
				id,number,token_id,hot_wallet_id,total_amount,status
			) VALUES
				(1,$1,$3,$5,$6,100000,'held'),
				(1,$2,$4,$5,$6,100000,'held')
		`, sweepID, expiredSweepID, "CS-RR-"+suffix, "CS-RR-EXPIRED-"+suffix,
			tokenID, hotWalletID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO reg_crypto_sweep_attempts (
				id,wallet_id,token_id,network_id,sweep_id,hot_wallet_id,
				source_address,destination_address,amount,status,error_message,expires_at,updated_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,$7,$8,100000,'held','activation transaction rolled back',
				NOW()+interval '10 minutes',NOW()-interval '1 minute'
			)
		`, attemptID, poolWalletID, tokenID, networkID, sweepID, hotWalletID,
			"TPool"+suffix, "THot"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO reg_crypto_sweep_attempts (
				id,wallet_id,token_id,network_id,sweep_id,hot_wallet_id,
				source_address,destination_address,amount,status,error_message,expires_at,updated_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,$7,$8,100000,'held','resource rental expired before signing',
				NOW()+interval '10 minutes',NOW()-interval '1 minute'
			)
		`, expiredAttemptID, expiredPoolWalletID, tokenID, networkID, expiredSweepID,
			hotWalletID, "TExpiredPool"+suffix, "THot"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO doc_crypto_resource_rentals (
				id,number,date,basis_type,basis_id,provider_account_id,network_id,
				receive_wallet_id,cost_token_id,resource_type,rental_mode,rental_period,
				requested_amount,provider,out_trade_no,status,request_json,expires_at
			) VALUES (
				$1,$2,NOW(),'CryptoSweep',$3,$4,$5,$6,$7,'energy','fixed','1H',
				65000,'itrx',$8,'ordering','{}'::jsonb,NOW()+interval '1 hour'
			)
		`, rentalID, "RR-"+suffix, sweepID, providerAccountID,
			networkID, poolWalletID, nativeTokenID, "mp_rr_"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `
			INSERT INTO doc_crypto_resource_rentals (
				id,number,date,basis_type,basis_id,provider_account_id,network_id,
				receive_wallet_id,cost_token_id,resource_type,rental_mode,rental_period,
				requested_amount,delegated_amount,gross_cost_amount,
				provider,out_trade_no,external_serial,status,cost_status,request_json,
				activated_at,expires_at,closed_at
			) VALUES (
				$1,$2,NOW()-interval '2 hours','CryptoSweep',$3,$4,$5,$6,$7,
				'energy','fixed','1H',65000,65000,650000,'itrx',$8,$9,
				'closed','recorded','{}'::jsonb,NOW()-interval '2 hours',
				NOW()-interval '1 hour',NOW()-interval '1 hour'
			)
		`, expiredRentalID, "RR-EXPIRED-"+suffix, expiredSweepID, providerAccountID,
			networkID, expiredPoolWalletID, nativeTokenID, "mp_rr_expired_"+suffix,
			"provider-expired-"+suffix)
		require.NoError(t, err)

		repo := NewOutboundRepo()
		tasks, err := repo.ClaimRunnableSweepAttempts(txCtx, 100)
		require.NoError(t, err)
		require.True(t, containsSweepAttempt(tasks, attemptID))
		require.True(t, containsSweepAttempt(tasks, expiredAttemptID))

		var status, expiredStatus string
		require.NoError(t, q.QueryRow(txCtx,
			`SELECT status FROM reg_crypto_sweep_attempts WHERE id=$1`, attemptID).Scan(&status))
		require.Equal(t, "claimed", status)
		require.NoError(t, q.QueryRow(txCtx,
			`SELECT status FROM reg_crypto_sweep_attempts WHERE id=$1`, expiredAttemptID).Scan(&expiredStatus))
		require.Equal(t, "claimed", expiredStatus)

		fundingID := id.New()
		_, err = q.Exec(txCtx, `
			INSERT INTO doc_crypto_native_fundings (
				id,basis_type,basis_id,attempt,network_id,native_token_id,
				source_wallet_id,destination_wallet_id,amount_minor,status,last_error
			) VALUES (
				$1,'CryptoSweepAttempt',$2,1,$3,$4,$5,$6,21000000,'held',
				'fee wallet residual is insufficient'
			)
		`, fundingID, attemptID, networkID, nativeTokenID, hotWalletID, poolWalletID)
		require.NoError(t, err)
		require.NoError(t, (nativeFundingStore{OutboundRepo: NewOutboundRepo()}).RetryBeforeAuthorization(txCtx, fundingID))
		var fundingStatus, fundingError string
		require.NoError(t, q.QueryRow(txCtx, `
			SELECT status,last_error FROM doc_crypto_native_fundings WHERE id=$1
		`, fundingID).Scan(&fundingStatus, &fundingError))
		require.Equal(t, "created", fundingStatus)
		require.Empty(t, fundingError)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}

func containsSweepAttempt(tasks []SweepTask, attemptID id.ID) bool {
	for _, task := range tasks {
		if task.AttemptID == attemptID {
			return true
		}
	}
	return false
}
