//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestResourceAllocationNewGenerationAndExactBroadcastRecovery(t *testing.T) {
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
	errRollback := errors.New("rollback integration fixture")

	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		networkID, nativeTokenID, tokenID := id.New(), id.New(), id.New()
		walletID, providerID, rentalID := id.New(), id.New(), id.New()
		oldAllocationID, attemptID, authID := id.New(), id.New(), id.New()
		suffix := id.Compact(networkID)
		txHash := "resource-recovery-" + suffix
		_, fixtureErr := q.Exec(txCtx, `
			INSERT INTO cat_blockchain_networks(
				id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,confirmations_needed)
			VALUES($1,$2,'Resource recovery network','tron',$3,'TRX',6,1)
		`, networkID, "RA-NET-"+suffix, "resource-recovery-"+suffix)
		require.NoError(t, fixtureErr)
		_, fixtureErr = q.Exec(txCtx, `
			INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
			VALUES
				($1,$2,'Resource recovery TRX',$3,'','TRX',6,'native'),
				($4,$5,'Resource recovery USDT',$3,$6,'USDT',6,'TRC-20')
		`, nativeTokenID, "RA-TRX-"+suffix, networkID, tokenID, "RA-USDT-"+suffix, "TContract"+suffix)
		require.NoError(t, fixtureErr)
		_, fixtureErr = q.Exec(txCtx, `
			INSERT INTO cat_wallets(
				id,code,name,network_id,address,tier,wallet_role,status,allocation_mode,vault_key_id,chain_activation_status)
			VALUES($1,$2,'Resource recovery wallet',$3,$4,'pool','merchant_pool','sweeping','transient',$5,'active')
		`, walletID, "RA-WALLET-"+suffix, networkID, "TWallet"+suffix, "vault/resource/"+suffix)
		require.NoError(t, fixtureErr)
		_, fixtureErr = q.Exec(txCtx, `
			INSERT INTO cat_crypto_provider_accounts(
				id,code,name,provider,network_id,balance_token_id,is_active)
			VALUES($1,$2,'Resource recovery provider','itrx',$3,$4,TRUE)
		`, providerID, "RA-PROVIDER-"+suffix, networkID, nativeTokenID)
		require.NoError(t, fixtureErr)
		_, fixtureErr = q.Exec(txCtx, `
			INSERT INTO doc_crypto_resource_rentals(
				id,number,basis_type,basis_id,posted,posted_version,provider_account_id,network_id,
				receive_wallet_id,cost_token_id,requested_amount,delegated_amount,gross_cost_amount,
				unused_cost_amount,provider,out_trade_no,external_serial,status,cost_status,expires_at)
			VALUES($1,$2,'CryptoSweep',$3,TRUE,1,$4,$5,$6,$7,65000,65000,650000,
			       650000,'itrx',$8,$9,'active','recorded',NOW()+interval '1 hour')
		`, rentalID, "RR-"+suffix, attemptID, providerID, networkID, walletID, nativeTokenID,
			"rr-"+suffix, "serial-"+suffix)
		require.NoError(t, fixtureErr)
		_, fixtureErr = q.Exec(txCtx, `
			INSERT INTO reg_crypto_resource_allocations(
				id,rental_id,basis_type,basis_id,purpose,operation_token_id,source_wallet_id,
				resource_type,generation,status,reserved_amount,released_at)
			VALUES($1,$2,'CryptoSweepLine',$3,'sweep',$4,$5,'energy',1,'released',40770,NOW())
		`, oldAllocationID, rentalID, attemptID, tokenID, walletID)
		require.NoError(t, fixtureErr)

		repo := NewOutboundRepo()
		allocation, _, reserveErr := repo.TryReserveResource(txCtx, ResourceRequest{
			BasisType: "CryptoSweepLine", BasisID: attemptID,
			Purpose: crypto_platform_cost.PlatformCostPurposeSweep, NetworkID: networkID,
			OperationTokenID: tokenID, SourceWalletID: walletID,
		}, 40770, time.Minute)
		require.NoError(t, reserveErr)
		require.NotNil(t, allocation)
		require.Equal(t, 2, allocation.Generation)
		require.NotEqual(t, oldAllocationID, allocation.ID)
		require.NoError(t, repo.ReleaseResourceAllocation(txCtx, allocation.ID, "pre-sign retry"))

		_, fixtureErr = q.Exec(txCtx, `
			INSERT INTO reg_crypto_signing_authorizations(
				id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,
				chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,
				destination_address,asset_standard,contract_address,amount_minor_units,
				max_network_fee_minor,basis_type,basis_id,attempt,generation,expires_at,
				policy_version,status,authorization_jws,tx_hash,unsigned_fingerprint,
				unsigned_envelope,signed_envelope)
			VALUES($1,$2,$3,'sweep',1,$4,$5,'tron',$6,$7,$7,$8,$9,'TRC-20',$10,100000,
			       20000000,'CryptoSweepAttempt',$11,1,1,NOW()+interval '10 minutes',
			       'tron-v1','broadcast','test-jws',$12,$13,'{}'::jsonb,'{}'::jsonb)
		`, authID, "auth-"+suffix, id.New().String(), networkID, "RA-NET-"+suffix,
			"resource-recovery-"+suffix, "TWallet"+suffix, "vault/resource/"+suffix,
			"THot"+suffix, "TContract"+suffix, attemptID, txHash, "fp-"+suffix)
		require.NoError(t, fixtureErr)
		_, fixtureErr = q.Exec(txCtx, `
			INSERT INTO reg_crypto_expected_chain_effects(
				signing_authorization_id,ordinal,network_id,event_kind,source_address_key,
				destination_address_key,token_id,contract_address_key,amount,tx_hash)
			VALUES($1,0,$2,'contract_log',$3,$4,$5,$6,100000,$7)
		`, authID, networkID, "TWallet"+suffix, "THot"+suffix, tokenID, "TContract"+suffix, txHash)
		require.NoError(t, fixtureErr)

		recovery := ResourceBroadcastCommand{BasisType: "CryptoSweepLine", BasisID: attemptID, TxHash: "wrong-" + txHash}
		require.ErrorContains(t, repo.MarkResourceAllocationBroadcast(txCtx, recovery), "exact signed")
		recovery.TxHash = txHash
		require.NoError(t, repo.MarkResourceAllocationBroadcast(txCtx, recovery))

		receipt := crypto.ChainReceipt{
			NetworkID: networkID, TxHash: txHash, Status: crypto.ReceiptSucceeded,
			Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified,
			Fee: crypto.FeeSettlementReport{Components: []crypto.FeeComponent{{
				Name: "energy_fee", Quantity: 29650, Amount: types.ZeroRawAmount(),
			}}},
		}
		require.NoError(t, repo.SettleResourceAllocation(txCtx, txHash, receipt))

		var generation int
		var status string
		var actual int64
		var allocated types.RawAmount
		require.NoError(t, q.QueryRow(txCtx, `
			SELECT generation,status,actual_amount,allocated_cost_amount
			FROM reg_crypto_resource_allocations WHERE tx_hash=$1
		`, txHash).Scan(&generation, &status, &actual, &allocated))
		require.Equal(t, 2, generation)
		require.Equal(t, "confirmed", status)
		require.EqualValues(t, 29650, actual)
		require.Equal(t, "296500", allocated.String())
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}
