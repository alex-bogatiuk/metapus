//go:build integration

package crypto_worker

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

func TestFeeBudgetConcurrentReservationsCannotOversubscribe(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required and must point to a disposable database")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	networkID, tokenID := id.New(), id.New()
	payerWalletID, destinationWalletID := id.New(), id.New()
	suffix := id.Compact(networkID)
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks
		(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
		VALUES ($1,$2,'Fee budget concurrency','tron',$3,'TRX',6)`, networkID, "BUDGET-"+suffix, "budget-"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens
		(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
		VALUES ($1,$2,'Native',$3,'TRX',6,'native','')`, tokenID, "NATIVE-"+suffix, networkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets
		(id,code,name,network_id,address,tier,status,vault_key_id,chain_activation_status,wallet_role)
		VALUES
		($1,$2,'Payer',$3,$4,'hot','free','test/payer','active','liquidity_hot'),
		($5,$6,'Destination',$3,$7,'pool','free','test/destination','active','merchant_pool')`,
		payerWalletID, "PAYER-"+suffix, networkID, "TPayer"+suffix,
		destinationWalletID, "DEST-"+suffix, "TDestination"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_balance_balances (wallet_id,token_id,amount) VALUES ($1,$2,1000)`, payerWalletID, tokenID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_network_policies SET
		enabled=TRUE,per_transaction_max_fee_minor=100,daily_network_fee_budget_minor=100,
		fee_funding_mode='source_only',daily_jit_funding_budget_minor=0,
		min_fee_wallet_balance_minor=0,min_source_wallet_residual_minor=0,
		jit_funding_max_amount_minor=0
		WHERE network_id=$1`, networkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_external_resource_policies
		(network_id,resource_strategy,provider_account_ids,provider_failure_threshold,provider_open_seconds)
		VALUES($1,'prefer_rental',ARRAY[$2]::uuid[],3,60)
		ON CONFLICT (network_id) DO UPDATE SET resource_strategy=EXCLUDED.resource_strategy,
		provider_account_ids=EXCLUDED.provider_account_ids,provider_failure_threshold=EXCLUDED.provider_failure_threshold,
		provider_open_seconds=EXCLUDED.provider_open_seconds,updated_at=NOW()`, networkID, id.New())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE sys_crypto_network_policies SET enabled=FALSE WHERE network_id=$1`, networkID)
	})
	// This test commits concurrent transactions and therefore cannot be wrapped in
	// a rollback-only fixture. Reservation events are intentionally append-only;
	// the disposable integration database is destroyed after the test run.

	txm := sdktest.NewTxManager(pool)
	manager := NewFeeBudgetManager()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			callCtx := tenant.WithTxManager(context.Background(), txm)
			_, reserveErr := manager.Reserve(callCtx, FeeReservationRequest{
				NetworkID: networkID, PayerWalletID: payerWalletID,
				NativeTokenID: tokenID, OperationTokenID: tokenID,
				BasisType: "ConcurrencyTest", BasisID: id.New(), Purpose: crypto.SigningPurposeSweep,
				Attempt: 1, Strategy: crypto.FeeStrategyNativeBurn, MaxFee: types.NewRawAmountFromInt64(70),
			})
			errs <- reserveErr
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	succeeded := 0
	for reserveErr := range errs {
		if reserveErr == nil {
			succeeded++
		}
	}
	require.Equal(t, 1, succeeded, "one of two 70-unit reservations must be rejected by a 100-unit daily budget")
	var committed int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(max_fee_minor),0)
		FROM reg_crypto_fee_reservations
		WHERE network_id=$1 AND status IN ('reserved','broadcast','settled','held')
	`, networkID).Scan(&committed))
	require.LessOrEqual(t, committed, int64(100))
}

func TestFeeBudgetRejectsInsufficientUnsignedBalanceWithoutPanic(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	networkID, tokenID, payerWalletID := id.New(), id.New(), id.New()
	suffix := id.Compact(networkID)
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks
		(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
		VALUES ($1,$2,'Unsigned balance guard','tron',$3,'TRX',6)`, networkID, "UNSIGNED-"+suffix, "unsigned-"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens
		(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
		VALUES ($1,$2,'Native',$3,'TRX',6,'native','')`, tokenID, "UNSIGNED-NATIVE-"+suffix, networkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets
		(id,code,name,network_id,address,tier,status,vault_key_id,chain_activation_status,wallet_role)
		VALUES ($1,$2,'Empty payer',$3,$4,'pool','sweeping','test/empty','active','merchant_pool')`,
		payerWalletID, "UNSIGNED-PAYER-"+suffix, networkID, "TEmpty"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_network_policies SET
		enabled=TRUE,per_transaction_max_fee_minor=100,daily_network_fee_budget_minor=200,
		fee_funding_mode='source_only',daily_jit_funding_budget_minor=0,
		min_fee_wallet_balance_minor=0,min_source_wallet_residual_minor=1,
		jit_funding_max_amount_minor=0
		WHERE network_id=$1`, networkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_external_resource_policies
		(network_id,resource_strategy,provider_account_ids,provider_failure_threshold,provider_open_seconds)
		VALUES($1,'burn_only',ARRAY[]::uuid[],3,60)
		ON CONFLICT (network_id) DO UPDATE SET resource_strategy=EXCLUDED.resource_strategy,
		provider_account_ids=EXCLUDED.provider_account_ids,provider_failure_threshold=EXCLUDED.provider_failure_threshold,
		provider_open_seconds=EXCLUDED.provider_open_seconds,updated_at=NOW()`, networkID)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM reg_crypto_fee_reservations WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_wallets WHERE id=$1`, payerWalletID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_tokens WHERE id=$1`, tokenID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_network_runtime_status WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_external_resource_policies WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_network_policies WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_wallet_pool_policies WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_blockchain_networks WHERE id=$1`, networkID)
	})

	txm := sdktest.NewTxManager(pool)
	callCtx := tenant.WithTxManager(ctx, txm)
	manager := NewFeeBudgetManager()
	var reserveErr error
	require.NotPanics(t, func() {
		_, reserveErr = manager.Reserve(callCtx, FeeReservationRequest{
			NetworkID: networkID, PayerWalletID: payerWalletID,
			NativeTokenID: tokenID, OperationTokenID: tokenID,
			BasisType: "UnsignedBalanceTest", BasisID: id.New(), Purpose: crypto.SigningPurposeSweep,
			Attempt: 1, Strategy: crypto.FeeStrategyNativeBurn, MaxFee: types.NewRawAmountFromInt64(100),
		})
	})
	require.ErrorIs(t, reserveErr, ErrFeeBalanceResidual)

	var reservations int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_fee_reservations WHERE network_id=$1`, networkID).Scan(&reservations))
	require.Zero(t, reservations)
}

func TestNativeFundingCreationIsIdempotentUnderConcurrency(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	networkID, tokenID := id.New(), id.New()
	sourceWalletID, destinationWalletID, basisID := id.New(), id.New(), id.New()
	suffix := id.Compact(networkID)
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks
		(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
		VALUES ($1,$2,'Funding concurrency','tron',$3,'TRX',6)`, networkID, "FUNDING-"+suffix, "funding-"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens
		(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
		VALUES ($1,$2,'Native',$3,'TRX',6,'native','')`, tokenID, "FUND-NATIVE-"+suffix, networkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets
		(id,code,name,network_id,address,tier,status,vault_key_id,chain_activation_status,wallet_role)
		VALUES
		($1,$2,'Fee wallet',$3,$4,'hot','free','test/fee','active','network_fee'),
		($5,$6,'Pool wallet',$3,$7,'pool','free','test/pool','active','merchant_pool')`,
		sourceWalletID, "FEE-"+suffix, networkID, "TFee"+suffix,
		destinationWalletID, "POOL-"+suffix, "TPool"+suffix)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM doc_crypto_native_fundings WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_wallets WHERE id IN ($1,$2)`, sourceWalletID, destinationWalletID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_tokens WHERE id=$1`, tokenID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_network_runtime_status WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_external_resource_policies WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_network_policies WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_crypto_wallet_pool_policies WHERE network_id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_blockchain_networks WHERE id=$1`, networkID)
	})

	txm := sdktest.NewTxManager(pool)
	start := make(chan struct{})
	results := make(chan crypto.NativeFunding, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			callCtx := tenant.WithTxManager(context.Background(), txm)
			record, createErr := (nativeFundingStore{OutboundRepo: NewOutboundRepo()}).Create(callCtx, crypto.NativeFunding{
				ID: id.New(), BasisType: "ConcurrencyFunding", BasisID: basisID, Attempt: 1,
				NetworkID: networkID, NativeTokenID: tokenID,
				SourceWalletID: sourceWalletID, DestinationWalletID: destinationWalletID,
				Amount: types.NewRawAmountFromInt64(100),
			})
			results <- record
			errs <- createErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for createErr := range errs {
		require.NoError(t, createErr)
	}
	var persistedID id.ID
	for record := range results {
		if id.IsNil(persistedID) {
			persistedID = record.ID
		}
		require.Equal(t, persistedID, record.ID)
	}
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM doc_crypto_native_fundings WHERE basis_type='ConcurrencyFunding' AND basis_id=$1 AND attempt=1`, basisID).Scan(&count))
	require.Equal(t, 1, count)
}
