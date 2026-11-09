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
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestWithdrawalRecoveryReusesSignedAttemptAfterBroadcastBeforePostFailure(t *testing.T) {
	ctx, txm, closePool := cryptoIntegrationTxManager(t)
	defer closePool()
	errRollback := errors.New("rollback integration fixture")

	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		networkID, tokenID, merchantID, walletID, requestID := id.New(), id.New(), id.New(), id.New(), id.New()
		suffix := id.Compact(networkID)
		_, err := q.Exec(txCtx, `INSERT INTO cat_blockchain_networks
			(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
			VALUES($1,$2,'Broadcast recovery','tron',$3,'TRX',6)`, networkID, "BR-"+suffix, "br-"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_tokens
			(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
			VALUES($1,$2,'Recovery token',$3,'USDT',6,'TRC-20',$4)`, tokenID, "BR-TOKEN-"+suffix, networkID, "TContract"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Recovery merchant')`, merchantID, "BR-M-"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_wallets
			(id,code,name,network_id,address,tier,status,vault_key_id,chain_activation_status)
			VALUES($1,$2,'Recovery hot',$3,$4,'hot','free',$5,'active')`, walletID, "BR-W-"+suffix, networkID, "THot"+suffix, "vault/hot/"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO doc_withdrawal_requests
			(id,number,merchant_id,token_id,amount,dest_address,status,posted,posted_version,updated_at)
			VALUES($1,$2,$3,$4,100000,$5,'signing',TRUE,1,NOW()-interval '3 minutes')`,
			requestID, "WR-BR-"+suffix, merchantID, tokenID, "TDestination"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,100000)`, walletID, tokenID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_balance_reservations
			(id,wallet_id,token_id,amount,purpose,document_type,document_id,status)
			VALUES($1,$2,$3,100000,'withdrawal',$4,$5,'active')`, id.New(), walletID, tokenID, withdrawal_request.DocumentType, requestID)
		require.NoError(t, err)

		fingerprint := "fingerprint-" + suffix
		authID := "auth-" + suffix
		auth := crypto.SigningAuthorization{
			SchemaVersion: crypto.SigningAuthorizationSchemaVersionFor(crypto.SequenceNone), AuthorizationID: authID,
			TenantID: id.New().String(), MerchantID: &merchantID, Purpose: crypto.SigningPurposeWithdrawal,
			BasisType: withdrawal_request.DocumentType, BasisID: requestID, Attempt: 1, Generation: 1,
			ExpiresAt: time.Now().UTC().Add(10 * time.Minute), NetworkID: networkID,
			NetworkCode: "BR-" + suffix, ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: "br-" + suffix,
			SourceAddress: "THot" + suffix, FeePayerAddress: "THot" + suffix, VaultKeyID: "vault/hot/" + suffix,
			DestinationAddress: "TDestination" + suffix, AssetStandard: "TRC-20", ContractAddress: "TContract" + suffix,
			AmountMinorUnits: types.NewCryptoAmountFromInt64(100000), MaxNetworkFeeMinor: types.NewCryptoAmountFromInt64(1000),
			UnsignedFingerprint: fingerprint, ApprovalBundleDigest: "approval-digest",
			PolicyVersion: crypto.SigningAuthorizationPolicyVersion(crypto.ChainFamilyTRON),
		}
		unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: fingerprint, Payload: []byte(`{"raw":"tx"}`)}
		repo := NewOutboundRepo()
		_, err = repo.InsertSigningAuthorization(txCtx, auth, "authorization-jws", unsigned, crypto.ApprovalBundle{IntentJWS: "intent-jws"})
		require.NoError(t, err)
		signed := crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, UnsignedFingerprint: fingerprint, TxHash: "tx-" + suffix, Payload: []byte(`{"signed":"tx"}`)}
		require.NoError(t, repo.MarkSigningAuthorizationSigned(txCtx, authID, signed))
		// This is the durable state written when chain broadcast succeeded but
		// the following accounting/post transaction failed.
		require.NoError(t, repo.MarkSigningAuthorizationHeld(txCtx, authID, signed.TxHash))

		tasks, err := repo.ClaimApprovedWithdrawals(txCtx, 10)
		require.NoError(t, err)
		require.True(t, containsWithdrawalTask(tasks, requestID))
		_, recovered, ok, err := repo.GetWithdrawalSigningAttempt(txCtx, requestID)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, recovered.SignedTx)
		require.Equal(t, signed.TxHash, recovered.SignedTx.TxHash)
		require.Equal(t, fingerprint, recovered.SignedTx.UnsignedFingerprint)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}

func TestSweepRecoveryPreservesBroadcastStateForPostRetry(t *testing.T) {
	ctx, txm, closePool := cryptoIntegrationTxManager(t)
	defer closePool()
	errRollback := errors.New("rollback integration fixture")

	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		networkID, tokenID, hotWalletID, poolWalletID := id.New(), id.New(), id.New(), id.New()
		sweepID, attemptID := id.New(), id.New()
		suffix := id.Compact(networkID)
		_, err := q.Exec(txCtx, `INSERT INTO cat_blockchain_networks
			(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
			VALUES($1,$2,'Sweep broadcast recovery','tron',$3,'TRX',6)`, networkID, "SBR-"+suffix, "sbr-"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_tokens
			(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
			VALUES($1,$2,'Sweep recovery token',$3,'USDT',6,'TRC-20',$4)`, tokenID, "SBR-T-"+suffix, networkID, "TContract"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_wallets
			(id,code,name,network_id,address,tier,wallet_role,status,vault_key_id,chain_activation_status)
			VALUES($1,$3,'Sweep recovery hot',$5,$6,'hot','liquidity_hot','free',$8,'active'),
			      ($2,$4,'Sweep recovery pool',$5,$7,'pool','merchant_pool','sweeping',$9,'active')`,
			hotWalletID, poolWalletID, "SBR-H-"+suffix, "SBR-P-"+suffix, networkID,
			"THot"+suffix, "TPool"+suffix, "vault/hot/"+suffix, "vault/pool/"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO doc_crypto_sweeps(id,number,token_id,hot_wallet_id,total_amount,status)
			VALUES($1,$2,$3,$4,100000,'held')`, sweepID, "CS-SBR-"+suffix, tokenID, hotWalletID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_sweep_attempts
			(id,wallet_id,token_id,network_id,sweep_id,hot_wallet_id,source_address,destination_address,amount,status,broadcast_at,expires_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,100000,'broadcast',NOW()-interval '1 minute',NOW()+interval '10 minutes',NOW()-interval '1 minute')`,
			attemptID, poolWalletID, tokenID, networkID, sweepID, hotWalletID, "TPool"+suffix, "THot"+suffix)
		require.NoError(t, err)

		repo := NewOutboundRepo()
		tasks, err := repo.ClaimRunnableSweepAttempts(txCtx, 10)
		require.NoError(t, err)
		require.True(t, containsSweepAttempt(tasks, attemptID))
		var status string
		require.NoError(t, q.QueryRow(txCtx, `SELECT status FROM reg_crypto_sweep_attempts WHERE id=$1`, attemptID).Scan(&status))
		require.Equal(t, "broadcast", status)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}

func cryptoIntegrationTxManager(t *testing.T) (context.Context, *postgres.TxManager, func()) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	txm := postgres.NewTxManagerFromRawPool(pool)
	return tenant.WithTxManager(ctx, txm), txm, pool.Close
}

func containsWithdrawalTask(tasks []WithdrawalTask, requestID id.ID) bool {
	for _, task := range tasks {
		if task.RequestID == requestID {
			return true
		}
	}
	return false
}
