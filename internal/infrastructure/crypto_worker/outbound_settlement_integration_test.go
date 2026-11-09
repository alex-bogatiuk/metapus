//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestConcurrentOutboundSettlementEachKindCreatesOneMovementSet(t *testing.T) {
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

	networkID, nativeTokenID, operationTokenID := id.New(), id.New(), id.New()
	merchantID, feePayerID, sweepSourceID := id.New(), id.New(), id.New()
	withdrawalAddressID, treasuryAddressID := id.New(), id.New()
	requestID, withdrawalID := id.New(), id.New()
	sweepID, sweepLineID, treasuryID := id.New(), id.New(), id.New()
	suffix := id.Compact(networkID)
	withdrawalTx, sweepTx, treasuryTx := "settle-withdrawal-"+suffix, "settle-sweep-"+suffix, "settle-treasury-"+suffix
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks(
			id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
		VALUES($1,$2,'Concurrent outbound settlement','tron',$3,'TRX',6,FALSE);
		INSERT INTO cat_tokens(
			id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
		VALUES($4,$5,'Concurrent settlement native',$1,'TRX',6,'native',''),
		      ($6,$7,'Concurrent settlement operation',$1,$8,6,'TRC-20',$9);
		INSERT INTO cat_merchants(id,code,name)
		VALUES($10,$11,'Concurrent settlement merchant');
		INSERT INTO cat_wallets(
			id,code,name,network_id,address,address_key,tier,wallet_role,status,vault_key_id,chain_activation_status)
		VALUES($12,$13,'Settlement fee payer',$1,$14,$14,'hot','liquidity_hot','free',$15,'active'),
		      ($16,$17,'Settlement sweep source',$1,$18,$18,'pool','merchant_pool','sweeping',$19,'active');
		INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount)
		VALUES($12,$4,1000),($16,$4,1000);
		INSERT INTO reg_withdrawal_addresses(
			id,merchant_id,network_id,address,address_key,label)
		VALUES($20,$10,$1,$21,$21,'external withdrawal');
		INSERT INTO reg_treasury_withdrawal_addresses(
			id,network_id,address,address_key,label,is_active)
		VALUES($22,$1,$23,$23,'external treasury',TRUE)
	`, pgx.QueryExecModeSimpleProtocol, networkID, "SR-NET-"+suffix, "settle-race-"+suffix,
		nativeTokenID, "SR-NATIVE-"+suffix, operationTokenID,
		"SR-TOKEN-"+suffix, "SR"+suffix[:6], "TSettleContract"+suffix,
		merchantID, "SR-MERCHANT-"+suffix,
		feePayerID, "SR-PAYER-"+suffix, "TSettlePayer"+suffix, "vault/settle/payer/"+suffix,
		sweepSourceID, "SR-SOURCE-"+suffix, "TSettleSource"+suffix, "vault/settle/source/"+suffix,
		withdrawalAddressID, "TSettleWithdrawalDestination"+suffix,
		treasuryAddressID, "TSettleTreasuryDestination"+suffix)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO doc_crypto_withdrawals(accounting_model_version,id,number,basis_type,basis_id,posted,posted_version,merchant_id,token_id,source_wallet_id,
			dest_address,amount,tx_hash,status,settlement_status)
		VALUES(1,$7,$8,'WithdrawalRequest',$1,TRUE,1,$3,$4,$9,$5,100000,$10,'broadcast','repairing');
		INSERT INTO doc_withdrawal_requests(
			id,number,merchant_id,token_id,amount,dest_address,address_id,status,posted,posted_version,withdrawal_id)
		VALUES($1,$2,$3,$4,100000,$5,$6,'broadcast',TRUE,1,$7);
		INSERT INTO doc_crypto_sweeps(accounting_model_version,
			id,number,posted,posted_version,token_id,hot_wallet_id,total_amount,status)
		VALUES(1,$11,$12,TRUE,1,$4,$9,200000,'broadcast');
		INSERT INTO doc_crypto_sweep_lines(
			line_id,document_id,line_no,wallet_id,amount,tx_hash,settlement_status)
		VALUES($13,$11,1,$14,200000,$15,'repairing');
		INSERT INTO doc_treasury_withdrawals(
			accounting_model_version,id,number,posted,posted_version,token_id,source_wallet_id,dest_address,address_id,
			amount,tx_hash,status,settlement_status)
		VALUES(1,$16,$17,TRUE,1,$4,$9,$18,$19,300000,$20,'broadcast','repairing')
	`, pgx.QueryExecModeSimpleProtocol, requestID, "WR-"+suffix, merchantID, operationTokenID,
		"TSettleWithdrawalDestination"+suffix, withdrawalAddressID, withdrawalID,
		"CW-"+suffix, feePayerID, withdrawalTx,
		sweepID, "SWEEP-"+suffix, sweepLineID, sweepSourceID, sweepTx,
		treasuryID, "TREASURY-"+suffix, "TSettleTreasuryDestination"+suffix, treasuryAddressID, treasuryTx)
	require.NoError(t, err)

	tasks := []struct {
		task          outboundapp.SettlementTask
		reservationID id.ID
		basisType     string
		basisID       id.ID
		purpose       crypto.SigningPurpose
		payerWalletID id.ID
	}{
		{task: outboundapp.SettlementTask{Kind: outboundapp.SettlementWithdrawal, DocumentID: withdrawalID, RequestID: requestID, MerchantID: merchantID, NetworkID: networkID, OperationTokenID: operationTokenID, SourceWalletID: feePayerID, TxHash: withdrawalTx, Amount: types.NewCryptoAmountFromInt64(100000), PostedVersion: 1}, reservationID: id.New(), basisType: "WithdrawalRequest", basisID: requestID, purpose: crypto.SigningPurposeWithdrawal, payerWalletID: feePayerID},
		{task: outboundapp.SettlementTask{Kind: outboundapp.SettlementSweep, DocumentID: sweepID, LineID: sweepLineID, NetworkID: networkID, OperationTokenID: operationTokenID, SourceWalletID: sweepSourceID, HotWalletID: feePayerID, TxHash: sweepTx, Amount: types.NewCryptoAmountFromInt64(200000), PostedVersion: 1}, reservationID: id.New(), basisType: "CryptoSweepAttempt", basisID: sweepLineID, purpose: crypto.SigningPurposeSweep, payerWalletID: sweepSourceID},
		{task: outboundapp.SettlementTask{Kind: outboundapp.SettlementTreasury, DocumentID: treasuryID, NetworkID: networkID, OperationTokenID: operationTokenID, SourceWalletID: feePayerID, TxHash: treasuryTx, Amount: types.NewCryptoAmountFromInt64(300000), PostedVersion: 1}, reservationID: id.New(), basisType: "TreasuryWithdrawal", basisID: treasuryID, purpose: crypto.SigningPurposeTreasury, payerWalletID: feePayerID},
	}
	for _, fixture := range tasks {
		_, err = pool.Exec(ctx, `
			INSERT INTO reg_crypto_fee_reservations(accounting_model_version,
				id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,
				purpose,attempt,strategy,max_fee_minor,tx_hash,status,broadcast_at)
			VALUES(1,$1,$2,$3,$4,$5,$6,$7,$8,1,'native_burn',100,$9,'broadcast',NOW())
		`, fixture.reservationID, networkID, fixture.payerWalletID, nativeTokenID,
			operationTokenID, fixture.basisType, fixture.basisID, fixture.purpose, fixture.task.TxHash)
		require.NoError(t, err)
	}

	manager, repo := NewFeeBudgetManager(), NewOutboundRepo()
	for _, fixture := range tasks {
		fixture := fixture
		receipt := crypto.ChainReceipt{
			NetworkID: networkID, TxHash: fixture.task.TxHash, Status: crypto.ReceiptSucceeded,
			BlockHash: "block-" + fixture.task.TxHash, BlockHeight: 100,
			Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified,
			Fee: crypto.FeeSettlementReport{
				NativeAsset: crypto.AssetRef{TokenID: nativeTokenID, Standard: token.TokenStandardNative},
				TotalFee:    types.NewCryptoAmountFromInt64(7),
			},
			RawReceipt: []byte(`{"result":"SUCCESS"}`), ObservedAt: time.Now().UTC(),
		}
		start := make(chan struct{})
		errorsByWorker := make(chan error, 2)
		var group sync.WaitGroup
		for range 2 {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				errorsByWorker <- txm.RunInTransaction(ctx, func(txCtx context.Context) error {
					if err := repo.PrepareLegacyOutboundSettlement(txCtx, fixture.task); err != nil {
						return err
					}
					if settleErr := manager.Settle(txCtx, fixture.reservationID, receipt); settleErr != nil {
						return settleErr
					}
					return repo.RecordOutboundSettlement(txCtx, fixture.task, receipt)
				})
			}()
		}
		close(start)
		group.Wait()
		close(errorsByWorker)
		for settleErr := range errorsByWorker {
			require.NoError(t, settleErr)
		}
	}

	var settlements, walletMovements, platformMovements, settledReservations int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM reg_crypto_chain_settlements WHERE network_id=$1 AND tx_hash=ANY($2)),
		  (SELECT COUNT(*) FROM reg_crypto_balance_movements
		   WHERE recorder_type='ChainSettlement' AND line_id=ANY($3)),
		  (SELECT COUNT(*) FROM reg_crypto_platform_cost_movements
		   WHERE recorder_type='ChainSettlement' AND line_id=ANY($3)),
		  (SELECT COUNT(*) FROM reg_crypto_fee_reservations WHERE id=ANY($3) AND status='settled')
	`, networkID, []string{withdrawalTx, sweepTx, treasuryTx}, []id.ID{
		tasks[0].reservationID, tasks[1].reservationID, tasks[2].reservationID,
	}).Scan(&settlements, &walletMovements, &platformMovements, &settledReservations))
	require.Equal(t, 3, settlements)
	require.Equal(t, 3, walletMovements)
	require.Equal(t, 3, platformMovements)
	require.Equal(t, 3, settledReservations)

	var withdrawalStatus, requestStatus, sweepStatus, treasuryStatus string
	var withdrawalSettlement, sweepSettlement, treasurySettlement outbound_settlement.Status
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT withdrawal.status,request.status,withdrawal.settlement_status,
		       sweep.status,line.settlement_status,treasury.status,treasury.settlement_status
		FROM doc_crypto_withdrawals withdrawal
		JOIN doc_withdrawal_requests request ON request.withdrawal_id=withdrawal.id
		JOIN doc_crypto_sweeps sweep ON sweep.id=$2
		JOIN doc_crypto_sweep_lines line ON line.document_id=sweep.id
		JOIN doc_treasury_withdrawals treasury ON treasury.id=$3
		WHERE withdrawal.id=$1
	`, withdrawalID, sweepID, treasuryID).Scan(
		&withdrawalStatus, &requestStatus, &withdrawalSettlement,
		&sweepStatus, &sweepSettlement, &treasuryStatus, &treasurySettlement,
	))
	require.Equal(t, "confirmed", withdrawalStatus)
	require.Equal(t, "confirmed", requestStatus)
	require.Equal(t, outbound_settlement.StatusRecorded, withdrawalSettlement)
	require.Equal(t, "confirmed", sweepStatus)
	require.Equal(t, outbound_settlement.StatusRecorded, sweepSettlement)
	require.Equal(t, "confirmed", treasuryStatus)
	require.Equal(t, outbound_settlement.StatusRecorded, treasurySettlement)
}

func TestFailedWithdrawalCompensationIsAtomicAndIdempotent(t *testing.T) {
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
		networkID, nativeTokenID, operationTokenID := id.New(), id.New(), id.New()
		merchantID, walletID := id.New(), id.New()
		requestID, withdrawalID := id.New(), id.New()
		suffix := id.Compact(networkID)

		_, err := q.Exec(txCtx, `INSERT INTO cat_blockchain_networks
			(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
			VALUES($1,$2,'Settlement test','tron',$3,'TRX',6)`, networkID, "SETTLE-"+suffix, "settle-"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_tokens
			(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
			VALUES($1,$3,'Native',$2,'TRX',6,'native',''),
			      ($4,$5,'Operation',$2,'USDT',6,'TRC-20',$6)`,
			nativeTokenID, networkID, "NATIVE-"+suffix, operationTokenID, "TOKEN-"+suffix, "TContract"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Settlement merchant')`, merchantID, "MERCHANT-"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_wallets
			(id,code,name,network_id,address,tier,status,vault_key_id,chain_activation_status)
			VALUES($1,$2,'Settlement wallet',$3,$4,'hot','free',$5,'active')`,
			walletID, "WALLET-"+suffix, networkID, "TWallet"+suffix, "vault/"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO doc_withdrawal_requests
			(id,number,merchant_id,token_id,amount,dest_address,status,posted,posted_version)
			VALUES($1,$2,$3,$4,100000,$5,'broadcast',TRUE,1)`,
			requestID, "WR-"+suffix, merchantID, operationTokenID, "TDestination"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO doc_crypto_withdrawals
			(accounting_model_version,id,number,basis_type,basis_id,posted,posted_version,merchant_id,token_id,source_wallet_id,dest_address,
			 amount,tx_hash,status,settlement_status)
			VALUES(1,$1,$2,'WithdrawalRequest',$3,TRUE,1,$4,$5,$6,$7,100000,$8,'broadcast','repairing')`,
			withdrawalID, "CW-"+suffix, requestID, merchantID, operationTokenID, walletID, "TDestination"+suffix, "failedtx"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `UPDATE doc_withdrawal_requests SET withdrawal_id=$2 WHERE id=$1`, requestID, withdrawalID)
		if err != nil {
			return err
		}
		_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_merchant_balance_movements
			(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
			VALUES($1,$2,'WithdrawalRequest',1,NOW(),'expense',$3,$4,100000)`, id.New(), requestID, merchantID, operationTokenID)
		require.NoError(t, err)

		task := outboundapp.SettlementTask{
			Kind: outboundapp.SettlementWithdrawal, DocumentID: withdrawalID, RequestID: requestID,
			MerchantID: merchantID, NetworkID: networkID, OperationTokenID: operationTokenID,
			SourceWalletID: walletID, TxHash: "failedtx" + suffix, Amount: types.NewCryptoAmountFromInt64(100000), PostedVersion: 1,
		}
		_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
 VALUES($1,$2,'CryptoWithdrawal',1,NOW(),'expense',$3,$4,100000)`, id.New(), withdrawalID, walletID, operationTokenID)
		require.NoError(t, err)
		receipt := crypto.ChainReceipt{
			NetworkID: networkID, TxHash: task.TxHash, Status: crypto.ReceiptReverted,
			BlockHash: "block-1", BlockHeight: 1,
			Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified,
			Fee:        crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: nativeTokenID, Standard: token.TokenStandardNative}, TotalFee: types.ZeroCryptoAmount()},
			RawReceipt: []byte(`{"result":"OUT_OF_ENERGY"}`), ObservedAt: time.Now().UTC(),
		}

		repo := NewOutboundRepo()
		require.NoError(t, repo.RecordOutboundSettlement(txCtx, task, receipt))
		require.NoError(t, repo.RecordOutboundSettlement(txCtx, task, receipt))

		var balance int64
		require.NoError(t, q.QueryRow(txCtx, `SELECT amount FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=$2`, merchantID, operationTokenID).Scan(&balance))
		require.Zero(t, balance)
		var movementCount int
		require.NoError(t, q.QueryRow(txCtx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$1`, requestID).Scan(&movementCount))
		require.Equal(t, 2, movementCount)
		var settlementStatus outbound_settlement.Status
		var withdrawalStatus, requestStatus string
		require.NoError(t, q.QueryRow(txCtx, `SELECT settlement_status,status FROM doc_crypto_withdrawals WHERE id=$1`, withdrawalID).Scan(&settlementStatus, &withdrawalStatus))
		require.NoError(t, q.QueryRow(txCtx, `SELECT status FROM doc_withdrawal_requests WHERE id=$1`, requestID).Scan(&requestStatus))
		require.Equal(t, outbound_settlement.StatusRecorded, settlementStatus)
		require.Equal(t, "failed", withdrawalStatus)
		require.Equal(t, "failed", requestStatus)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}
