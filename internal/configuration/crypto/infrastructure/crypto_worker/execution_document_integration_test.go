//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"metapus/internal/configuration/crypto/domain/documents/crypto_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/outbound_settlement"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/tenant"
	"metapus/pkg/sdk/types"
)

func TestExecutionDraftsAreIdempotent(t *testing.T) {
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
		networkID, tokenID, merchantID := id.New(), id.New(), id.New()
		hotWalletID, poolWalletID := id.New(), id.New()
		requestID, attemptID := id.New(), id.New()
		_, err := q.Exec(txCtx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,'TEST-NET','Test Network','tron','test-chain','TRX',6)`, networkID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,'TEST-USDT','Test USDT',$2,'USDT',6,'TRC-20')`, tokenID, networkID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,'TEST-MERCHANT','Test Merchant')`, merchantID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_wallets(id,code,name,network_id,address,tier,status,vault_key_id,chain_activation_status) VALUES($1,'TEST-HOT','Test Hot',$3,'THot','hot','free','vault/hot','active'),($2,'TEST-POOL','Test Pool',$3,'TPool','pool','sweeping','vault/pool','active')`, hotWalletID, poolWalletID, networkID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO doc_withdrawal_requests(id,number,merchant_id,token_id,amount,dest_address,status,posted) VALUES($1,'WR-TEST',$2,$3,100000,'TDestination','signing',TRUE)`, requestID, merchantID, tokenID)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at) VALUES($1,$2,$3,$4,$5,'TPool','THot',100000,'claimed',NOW()+interval '10 minutes')`, attemptID, poolWalletID, tokenID, networkID, hotWalletID)
		require.NoError(t, err)

		outbound := NewOutboundRepo()
		withdrawalRepo := crypto_document_repo.NewCryptoWithdrawalRepo()
		withdrawalCandidate := crypto_withdrawal.NewCryptoWithdrawal(merchantID, tokenID, hotWalletID, "TDestination", types.NewRawAmountFromInt64(100000))
		withdrawalCandidate.Number = "CW-TEST-1"
		withdrawalCandidate.BasisType = withdrawal_request.DocumentType
		withdrawalCandidate.BasisID = &requestID
		firstWithdrawal, err := outbound.EnsureWithdrawalExecutionDocument(txCtx, requestID, withdrawalCandidate, withdrawalRepo)
		require.NoError(t, err)

		secondCandidate := crypto_withdrawal.NewCryptoWithdrawal(merchantID, tokenID, hotWalletID, "TDestination", types.NewRawAmountFromInt64(100000))
		secondCandidate.Number = "CW-TEST-2"
		secondCandidate.BasisType = withdrawal_request.DocumentType
		secondCandidate.BasisID = &requestID
		secondWithdrawal, err := outbound.EnsureWithdrawalExecutionDocument(txCtx, requestID, secondCandidate, withdrawalRepo)
		require.NoError(t, err)
		require.Equal(t, firstWithdrawal.ID, secondWithdrawal.ID)

		sweepRepo := crypto_document_repo.NewCryptoSweepRepo()
		sweepCandidate := crypto_sweep.NewCryptoSweep(tokenID, hotWalletID)
		sweepCandidate.Number = "CS-TEST-1"
		sweepCandidate.TotalAmount = types.NewRawAmountFromInt64(100000)
		sweepCandidate.SetLines([]crypto_sweep.CryptoSweepLine{{
			LineID: attemptID, LineNo: 1, WalletID: poolWalletID,
			Amount:     types.NewRawAmountFromInt64(100000),
			NetworkFee: types.ZeroRawAmount(), SettlementStatus: outbound_settlement.StatusNotApplicable,
		}})
		firstSweep, err := outbound.EnsureSweepExecutionDocument(txCtx, []id.ID{attemptID}, sweepCandidate, sweepRepo)
		require.NoError(t, err)
		secondSweep, err := outbound.EnsureSweepExecutionDocument(txCtx, []id.ID{attemptID}, nil, sweepRepo)
		require.NoError(t, err)
		require.Equal(t, firstSweep.ID, secondSweep.ID)
		require.Empty(t, firstSweep.BasisType)
		require.Nil(t, firstSweep.BasisID)

		var withdrawalCount, sweepCount, sweepLineCount int
		require.NoError(t, q.QueryRow(txCtx, `SELECT COUNT(*) FROM doc_crypto_withdrawals WHERE basis_type=$2 AND basis_id=$1`, requestID, withdrawal_request.DocumentType).Scan(&withdrawalCount))
		require.NoError(t, q.QueryRow(txCtx, `SELECT COUNT(*) FROM doc_crypto_sweeps WHERE id=$1`, firstSweep.ID).Scan(&sweepCount))
		require.NoError(t, q.QueryRow(txCtx, `SELECT COUNT(*) FROM doc_crypto_sweeps__lines WHERE document_id=$1`, firstSweep.ID).Scan(&sweepLineCount))
		require.Equal(t, 1, withdrawalCount)
		require.Equal(t, 1, sweepCount)
		require.Equal(t, 1, sweepLineCount)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}
