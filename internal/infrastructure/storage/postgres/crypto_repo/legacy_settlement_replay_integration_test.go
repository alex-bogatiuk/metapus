//go:build integration

package crypto_repo_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"
)

func legacySnapshot(t *testing.T, f crypto_repo.LegacySettlementFixture) string {
	t.Helper()
	var snapshot string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT jsonb_build_object(
 'physical',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_balance_movements m),
 'merchant',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_merchant_balance_movements m),
 'fees',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_fee_movements m),
 'withdrawal',(SELECT to_jsonb(d) FROM doc_crypto_withdrawals d WHERE id=$1),
 'sweep',(SELECT to_jsonb(d) FROM doc_crypto_sweep_lines d WHERE line_id=$2),
 'treasury',(SELECT to_jsonb(d) FROM doc_treasury_withdrawals d WHERE id=$1))::text`, f.Task.DocumentID, f.Task.LineID).Scan(&snapshot))
	return snapshot
}

func recordLegacy(f crypto_repo.LegacySettlementFixture, task outboundapp.SettlementTask, receipt crypto.ChainReceipt) error {
	return postgres.MustGetTxManager(f.Ctx).RunInTransaction(f.Ctx, func(ctx context.Context) error {
		r := outbound_repo.NewOutboundRepo()
		if err := r.PrepareLegacyOutboundSettlement(ctx, task); err != nil {
			return err
		}
		return r.RecordOutboundSettlement(ctx, task, receipt)
	})
}

func TestLegacySettlementConcurrentReplayChecksFullBasisAndOutcome(t *testing.T) {
	for _, kind := range []string{outboundapp.SettlementWithdrawal, outboundapp.SettlementSweep, outboundapp.SettlementTreasury} {
		t.Run(kind, func(t *testing.T) {
			f := crypto_repo.NewLegacySettlementFixture(t, kind, true)
			var wg sync.WaitGroup
			errs := make(chan error, 3)
			for range 3 {
				wg.Go(func() { errs <- recordLegacy(f, f.Task, f.Receipt) })
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			before := legacySnapshot(t, f)
			later := f.Receipt
			later.ObservedAt = later.ObservedAt.Add(24 * time.Hour)
			later.Confirmations += 10
			later.RawReceipt = []byte(`{"later":true}`)
			require.NoError(t, recordLegacy(f, f.Task, later))
			require.Equal(t, before, legacySnapshot(t, f))
			for _, change := range []string{"amount", "wallet", "token", "network", "version", "line"} {
				changed := f.Task
				switch change {
				case "amount":
					changed.Amount = types.MustCryptoAmount("101")
				case "wallet":
					changed.SourceWalletID = id.New()
				case "token":
					changed.OperationTokenID = f.Receipt.Fee.NativeAsset.TokenID
				case "network":
					changed.NetworkID = id.New()
				case "version":
					changed.PostedVersion++
				case "line":
					if kind != outboundapp.SettlementSweep {
						continue
					}
					changed.LineID = id.New()
				}
				require.ErrorContains(t, recordLegacy(f, changed, f.Receipt), "original basis or financial history", change)
			}
			changedReceipt := f.Receipt
			changedReceipt.Status = crypto.ReceiptSucceeded
			require.ErrorContains(t, recordLegacy(f, f.Task, changedReceipt), "original basis or financial history")
			changedReceipt = f.Receipt
			changedReceipt.Fee.TotalFee = types.MustCryptoAmount("1")
			require.ErrorContains(t, recordLegacy(f, f.Task, changedReceipt), "original basis or financial history")
			changedReceipt = f.Receipt
			changedReceipt.Fee.Components = []crypto.FeeComponent{{Name: "energy_fee", Quantity: 1, Amount: types.ZeroCryptoAmount()}}
			require.ErrorContains(t, recordLegacy(f, f.Task, changedReceipt), "original basis or financial history")
			require.Equal(t, before, legacySnapshot(t, f))
			var source string
			require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2`, f.Task.SourceWalletID, f.Task.OperationTokenID).Scan(&source))
			require.Equal(t, "0", source)
		})
	}
}

func TestLegacyCompensationRequiresTheExactOriginalDebits(t *testing.T) {
	for _, kind := range []string{outboundapp.SettlementWithdrawal, outboundapp.SettlementSweep, outboundapp.SettlementTreasury} {
		t.Run(kind, func(t *testing.T) {
			f := crypto_repo.NewLegacySettlementFixture(t, kind, false)
			before := legacySnapshot(t, f)
			require.ErrorContains(t, recordLegacy(f, f.Task, f.Receipt), "original basis or financial history")
			require.Equal(t, before, legacySnapshot(t, f))
		})
	}
	f := crypto_repo.NewLegacySettlementFixture(t, outboundapp.SettlementWithdrawal, true)
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount) VALUES($1,$2,'WithdrawalRequest',1,now(),'expense',$3,$4,1)`, id.New(), f.Task.RequestID, f.Task.MerchantID, f.Task.OperationTokenID)
	require.NoError(t, err)
	before := legacySnapshot(t, f)
	require.ErrorContains(t, recordLegacy(f, f.Task, f.Receipt), "original basis or financial history")
	require.Equal(t, before, legacySnapshot(t, f), "the latest arbitrary debit cannot become the refund basis")
}

func TestLegacyPendingOrForeignCompensationNeverCompletesPartialHistory(t *testing.T) {
	for _, mode := range []string{"matching_source", "foreign_source", "merchant", "hot", "partial_fees"} {
		t.Run(mode, func(t *testing.T) {
			kind := outboundapp.SettlementWithdrawal
			if mode == "hot" {
				kind = outboundapp.SettlementSweep
			}
			if mode == "partial_fees" {
				kind = outboundapp.SettlementTreasury
			}
			f := crypto_repo.NewLegacySettlementFixture(t, kind, true)
			basis := f.Task.DocumentID
			if kind == outboundapp.SettlementSweep {
				basis = f.Task.LineID
			}
			recorder := uuid.NewSHA1(basis, []byte("failed-outbound:"+f.Task.TxHash))
			line := uuid.NewSHA1(recorder, []byte("source-receipt"))
			var err error
			switch mode {
			case "merchant":
				line = uuid.NewSHA1(f.Task.RequestID, []byte("failed-withdrawal-merchant:"+f.Task.TxHash))
				_, err = f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount) VALUES($1,$2,'ForeignOwner',1,now(),'receipt',$3,$4,100)`, line, id.New(), f.Task.MerchantID, f.Task.OperationTokenID)
			case "partial_fees":
				_, err = f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_fee_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount)
 SELECT $1,$2,'ChainOutboundStorno',1,now(),'receipt',merchant_id,token_id,fee_type,amount FROM reg_crypto_fee_movements WHERE recorder_id=$3 ORDER BY amount LIMIT 1`, id.New(), recorder, f.Task.DocumentID)
			default:
				owner := recorder
				record := "receipt"
				wallet := f.Task.SourceWalletID
				if mode == "foreign_source" {
					owner = id.New()
				}
				if mode == "hot" {
					line = uuid.NewSHA1(recorder, []byte("hot-expense"))
					record = "expense"
					wallet = f.Task.HotWalletID
				}
				_, err = f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,$2,'ChainOutboundStorno',1,now(),$3,$4,$5,100)`, line, owner, record, wallet, f.Task.OperationTokenID)
			}
			require.NoError(t, err)
			before := legacySnapshot(t, f)
			require.ErrorContains(t, recordLegacy(f, f.Task, f.Receipt), "original basis or financial history")
			require.Equal(t, before, legacySnapshot(t, f))
		})
	}
}

func TestLegacySettlementOuterRollbackAndWalletBeforeWorkflowRow(t *testing.T) {
	f := crypto_repo.NewLegacySettlementFixture(t, outboundapp.SettlementWithdrawal, true)
	tm := postgres.MustGetTxManager(f.Ctx)
	repo := outbound_repo.NewOutboundRepo()
	before := legacySnapshot(t, f)
	rollback := errors.New("enclosing transaction failed")
	err := tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		require.NoError(t, repo.RecordOutboundSettlement(ctx, f.Task, f.Receipt))
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	require.Equal(t, before, legacySnapshot(t, f))
	f.AssertLockOrder(t, func(ctx context.Context) error {
		return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, f.Task.SourceWalletID)
	},
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM doc_crypto_withdrawals WHERE id=$1 FOR UPDATE`, f.Task.DocumentID)
			return err
		},
		func(ctx context.Context) error { return repo.RecordOutboundSettlement(ctx, f.Task, f.Receipt) })
}
