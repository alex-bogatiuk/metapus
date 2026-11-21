//go:build integration

package crypto_worker

import (
	"context"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/stretchr/testify/require"
)

func TestNativeFundingRecognitionFreezesBothSidesAndRejectsPaymentAlias(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
		funding, _, receipt := seedNativeFundingConfirmation(t, ctx, tm, f)
		service := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager())
		ok, err := service.Settle(ctx, funding, receipt)
		require.NoError(t, err)
		require.True(t, ok)
		q := tm.GetQuerier(ctx)
		var postings, sides, bindings, events int
		require.NoError(t, q.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM reg_crypto_custody_effect_postings WHERE recorder_id=$1),
			(SELECT count(*) FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_custody_effect_postings p ON p.id=r.posting_id WHERE p.recorder_id=$1),
			(SELECT count(*) FROM reg_crypto_native_funding_custody_bindings WHERE funding_id=$1),
			(SELECT count(*) FROM sys_outbox WHERE aggregate_id=$1 AND event_type='custody_recognized')`, funding).Scan(&postings, &sides, &bindings, &events))
		require.Equal(t, []int{1, 2, 1, 1}, []int{postings, sides, bindings, events})
		_, err = q.Exec(ctx, "UPDATE doc_crypto_native_fundings SET amount_minor=99 WHERE id=$1", funding)
		require.ErrorContains(t, err, "basis is frozen")
		_, err = q.Exec(ctx, "UPDATE doc_crypto_native_fundings SET status='failed' WHERE id=$1", funding)
		require.ErrorContains(t, err, "basis is frozen")
		_, err = q.Exec(ctx, `INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,event_kind,event_index,amount,status)
			VALUES($1,$2,$3,$4,$5,$6,$7,'native_value',99,100,'confirming')`, id.New(), id.New(), id.New(), f.network, f.native, f.wallet, receipt.TxHash)
		require.ErrorContains(t, err, "belongs to funding settlement")
		// A synthetic recorder reaches the funding compensation guard; a
		// CryptoAdjustment without its document is rejected earlier by schema 88.
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id)
			SELECT $1,'TestFundingCompensation',1,now(),'expense',wallet_id,token_id,amount,line_id FROM reg_crypto_balance_movements
			WHERE recorder_id=$2 AND recorder_type='CryptoNativeFunding' AND record_type='receipt'`, id.New(), funding)
		require.ErrorContains(t, err, "explicit compensation workflow")
	})
}

type incompleteNativeFundingRepo struct {
	nativeFundingSettlementRepo
	component string
}

func (r incompleteNativeFundingRepo) PrepareNativeFundingPrincipal(ctx context.Context, f crypto.NativeFunding, receipt crypto.ChainReceipt) error {
	if r.component == "claim" {
		return nil
	}
	if err := r.nativeFundingSettlementRepo.PrepareNativeFundingPrincipal(ctx, f, receipt); err != nil {
		return err
	}
	if r.component == "outbox" {
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "DELETE FROM sys_outbox WHERE aggregate_id=$1 AND event_type='custody_recognized'", f.ID)
		return err
	}
	return nil
}

func (r incompleteNativeFundingRepo) RecordNativeFundingPrincipal(ctx context.Context, f crypto.NativeFunding, receipt crypto.ChainReceipt) error {
	if r.component != "destination" {
		return r.nativeFundingSettlementRepo.RecordNativeFundingPrincipal(ctx, f, receipt)
	}
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		SELECT r.movement_line_id,$1,'CryptoNativeFunding',1,$2,'expense',r.wallet_id,r.token_id,$3 FROM reg_crypto_custody_effect_recognitions r
		JOIN reg_crypto_custody_effect_postings p ON p.id=r.posting_id WHERE p.recorder_id=$1 AND r.record_type='expense'`, f.ID, receipt.ObservedAt, f.Amount)
	return err
}

func TestNativeFundingRecognitionRollsBackIncompleteSets(t *testing.T) {
	for _, component := range []string{"claim", "destination", "outbox", "proof", "orphan"} {
		t.Run(component, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
				funding, _, receipt := seedNativeFunding(t, ctx, tm, f, 1, component != "proof")
				if component == "orphan" {
					proof := nativeFundingProof(t, ctx, tm, funding, receipt)
					proof.AccountingIdentity = nil
					proof.Event.Canonicality, proof.Event.EventType = crypto.CanonicalityOrphaned, crypto.EventTypeReorg
					proof.Outcome, proof.ExecutionStatus = "absent", crypto.ReceiptPending
					proof.VerifiedAt = proof.VerifiedAt.Add(time.Second)
					_, err := crypto_repo.NewBlockIngestionRepo().SaveVerification(ctx, proof)
					require.NoError(t, err)
				}
				service := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, incompleteNativeFundingRepo{component: component}, NewFeeBudgetManager())
				ok, err := service.Settle(ctx, funding, receipt)
				require.Error(t, err)
				require.False(t, ok)
				var money, claims, outbox int
				var status string
				require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT
					(SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1),
					(SELECT count(*) FROM reg_crypto_custody_effect_postings WHERE recorder_id=$1),
					(SELECT count(*) FROM sys_outbox WHERE aggregate_id=$1),status FROM doc_crypto_native_fundings WHERE id=$1`, funding).Scan(&money, &claims, &outbox, &status))
				require.Equal(t, []int{0, 0, 0}, []int{money, claims, outbox})
				require.Equal(t, "broadcast", status)
			})
		})
	}
}

func TestNativeFundingFeeCannotReleasePrincipalBeforeRecognition(t *testing.T) {
	for _, feeAmount := range []int64{0, 2} {
		runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
			funding, _, receipt := seedNativeFundingConfirmation(t, ctx, tm, f)
			receipt.Fee.TotalFee = types.NewRawAmountFromInt64(feeAmount)
			receipt = reproveNetworkFee(t, ctx, receipt)
			var reservation id.ID
			require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT fee_reservation_id FROM doc_crypto_native_fundings WHERE id=$1", funding).Scan(&reservation))
			err := NewFeeBudgetManager().Settle(ctx, reservation, receipt)
			require.ErrorContains(t, err, "atomic principal recognition")
			var status string
			require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT status FROM reg_crypto_fee_reservations WHERE id=$1", reservation).Scan(&status))
			require.Equal(t, "broadcast", status)
			settled, err := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager()).Settle(ctx, funding, receipt)
			require.NoError(t, err)
			require.True(t, settled)
		})
	}
}
