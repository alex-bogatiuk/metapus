//go:build integration

package crypto_repo

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/stretchr/testify/require"
)

func adjustmentService() *cryptoapp.FinancialAdjustmentService {
	return cryptoapp.NewFinancialAdjustmentService(postgres.NewContextTxManager(), NewFinancialAdjustmentRepo())
}

func settledAdjustmentFixture(t *testing.T) paymentAttributionFixture {
	t.Helper()
	f := newPaymentAttributionFixture(t)
	require.NoError(t, f.post(f.payment, f.posting))
	_, err := f.pool.Exec(f.ctx, "UPDATE doc_crypto_payments SET status='settled' WHERE id=$1", f.payment.ID)
	require.NoError(t, err)
	return f
}

func adjustmentCommand(f paymentAttributionFixture) cryptoapp.ProposeFinancialAdjustmentCommand {
	return cryptoapp.ProposeFinancialAdjustmentCommand{Mode: "payment_reversal", PaymentID: f.payment.ID, PostedVersion: 1,
		ReasonCode: "incorrect_payment_posting", Evidence: json.RawMessage(`{"description":"verified correction of source movements"}`),
		MakerID: id.New(), IdempotencyKey: id.New().String()}
}

func TestAdjustmentBasisPartialCompensationReturnsAttribution(t *testing.T) {
	f := settledAdjustmentFixture(t)
	service, cmd := adjustmentService(), adjustmentCommand(f)
	sources, err := service.PaymentSources(f.ctx, f.payment.ID)
	require.NoError(t, err)
	for _, s := range sources.Lines {
		amount := map[string]string{"suspense": "10", "merchant": "8", "fee": "2"}[s.Register]
		cmd.SourceLines = append(cmd.SourceLines, cryptoapp.FinancialAdjustmentSourceSelection{Register: s.Register, LineID: s.LineID, Amount: amount})
	}
	first, err := service.Propose(f.ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, "payment_attribution", first.BasisKind)
	replay, err := service.Propose(f.ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, first.ID, replay.ID)
	foreign := cmd
	foreign.MakerID = id.New()
	_, err = service.Propose(f.ctx, foreign)
	require.ErrorContains(t, err, "idempotency")
	_, err = service.Approve(f.ctx, first.ID, id.New(), "checked partial financial correction")
	require.NoError(t, err)
	require.Equal(t, []string{"10", "12", "1"}, f.balances(t))
	second, err := service.Propose(f.ctx, adjustmentCommand(f))
	require.NoError(t, err)
	require.Equal(t, first.BasisKind, second.BasisKind)
	_, err = service.Approve(f.ctx, second.ID, id.New(), "checked remaining source quantities")
	require.NoError(t, err)
	require.Equal(t, []string{"23", "0", "0"}, f.balances(t))
	require.Equal(t, "23", f.custody(t), "a payment reversal never moves custody")
	var postings, corrections int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_custody_effect_postings),
		(SELECT count(*) FROM sys_outbox WHERE event_type='financial_correction_posted')`).Scan(&postings, &corrections))
	require.Equal(t, []int{1, 2}, []int{postings, corrections})
	_, err = f.pool.Exec(f.ctx, "UPDATE doc_crypto_adjustments SET basis_kind='wallet_variance' WHERE id=$1", first.ID)
	require.ErrorContains(t, err, "immutable")
}

func TestAdjustmentBasisConcurrentDecisionsCannotExceedSource(t *testing.T) {
	f := settledAdjustmentFixture(t)
	service := adjustmentService()
	a, err := service.Propose(f.ctx, adjustmentCommand(f))
	require.NoError(t, err)
	b, err := service.Propose(f.ctx, adjustmentCommand(f))
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Go(func() {
			target := a.ID
			if i%2 == 1 {
				target = b.ID
			}
			_, err := service.Approve(f.ctx, target, id.New(), "concurrent source correction review")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			require.NotContains(t, err.Error(), "deadlock")
			require.NotContains(t, err.Error(), "timeout")
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, []string{"23", "0", "0"}, f.balances(t))
	var posted, events int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM doc_crypto_adjustments WHERE status='posted'),
		(SELECT count(*) FROM sys_outbox WHERE event_type='financial_correction_posted')`).Scan(&posted, &events))
	require.Equal(t, []int{1, 1}, []int{posted, events})
}

// This helper writes a complete valid set first, allowing each omitted or
// corrupted component below to test the intended deferred guard in isolation.
func writeAdjustmentSet(ctx context.Context, a *cryptoapp.FinancialAdjustment, fault string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	for _, source := range adjustmentRegisters {
		if fault == "missing_"+source.kind {
			continue
		}
		dimensions := source.dimensions
		recorder := "CryptoAdjustment"
		if fault == "spoof_recorder" {
			recorder = "OtherAdjustment"
		}
		query := fmt.Sprintf(`INSERT INTO %s(line_id,recorder_id,recorder_type,recorder_version,period,record_type,%s,amount,reversal_of_line_id,reversal_reason)
			SELECT $3,$1,$4,1,now(),CASE WHEN m.record_type='receipt' THEN 'expense' ELSE 'receipt' END,%s,s.amount,m.line_id,'manual_financial_correction'
			FROM reg_crypto_adjustment_sources s JOIN %s m ON m.line_id=s.source_line_id
			WHERE s.adjustment_id=$1 AND s.source_register=$2`, source.table, dimensions,
			qualifiedAdjustmentDimensions(dimensions), source.table)
		if _, err := q.Exec(ctx, query, a.ID, source.kind, id.New(), recorder); err != nil {
			return err
		}
	}
	if fault != "pending" {
		if _, err := q.Exec(ctx, "UPDATE doc_crypto_adjustments SET status='posted',checker_id=$2,decision_reason='checked financial set',decided_at=now() WHERE id=$1", a.ID, id.New()); err != nil {
			return err
		}
	}
	if fault == "missing_outbox" {
		return nil
	}
	basisKind := a.BasisKind
	if fault == "wrong_outbox" {
		basisKind = "wallet_variance"
	}
	return postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: "CryptoAdjustment", AggregateID: a.ID, EventType: "financial_correction_posted",
		Payload: map[string]any{"entityType": "CryptoAdjustment", "entityId": a.ID, "action": "financial_correction_posted",
			"basisKind": basisKind, "postedVersion": a.PostedVersion}})
}

func qualifiedAdjustmentDimensions(dimensions string) string {
	switch dimensions {
	case "wallet_id,token_id":
		return "m.wallet_id,m.token_id"
	case "receipt_id,token_id":
		return "m.receipt_id,m.token_id"
	case "merchant_id,token_id":
		return "m.merchant_id,m.token_id"
	default:
		return "m.merchant_id,m.token_id,m.fee_type"
	}
}

func TestAdjustmentBasisRequiresCompleteAtomicSet(t *testing.T) {
	for _, fault := range []string{"valid", "missing_suspense", "missing_merchant", "missing_fee", "missing_outbox", "wrong_outbox", "pending", "spoof_recorder"} {
		t.Run(fault, func(t *testing.T) {
			f := settledAdjustmentFixture(t)
			a, err := adjustmentService().Propose(f.ctx, adjustmentCommand(f))
			require.NoError(t, err)
			err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error { return writeAdjustmentSet(ctx, a, fault) })
			if fault == "valid" {
				require.NoError(t, err)
				require.Equal(t, []string{"23", "0", "0"}, f.balances(t))
				return
			}
			require.Error(t, err)
			if fault == "missing_outbox" || fault == "wrong_outbox" {
				require.ErrorContains(t, err, "accounting basis outbox")
			}
			require.Equal(t, []string{"0", "20", "3"}, f.balances(t))
			stored, err := adjustmentService().Get(f.ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, "pending_approval", stored.Status)
		})
	}
}

func TestAdjustmentBasisSourcesFreezeAtProposal(t *testing.T) {
	f := settledAdjustmentFixture(t)
	a, err := adjustmentService().Propose(f.ctx, adjustmentCommand(f))
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_adjustment_sources(adjustment_id,source_register,source_line_id,amount)
		SELECT $1,source_register,source_line_id,1 FROM reg_crypto_adjustment_sources WHERE adjustment_id=$1 LIMIT 1`, a.ID)
	require.ErrorContains(t, err, "frozen at proposal")
	_, err = f.pool.Exec(f.ctx, "DELETE FROM doc_crypto_adjustments WHERE id=$1", a.ID)
	require.ErrorContains(t, err, "history is immutable")
}

func TestAdjustmentBasisWalletVarianceDoesNotRecognizeUserHash(t *testing.T) {
	f := newPaymentAttributionFixture(t)
	raw, err := json.Marshal(map[string]any{"description": "independent wallet variance evidence", "txHash": f.payment.TxHash, "verificationId": f.payment.VerificationID})
	require.NoError(t, err)
	cmd := cryptoapp.ProposeFinancialAdjustmentCommand{Mode: "wallet_balance", WalletID: f.payment.WalletID, TokenID: f.payment.TokenID,
		Direction: "receipt", Amount: "7", MakerID: id.New(), Direct: true, ReasonCode: "wallet_other", Evidence: raw, IdempotencyKey: id.New().String()}
	a, err := adjustmentService().Propose(f.ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, "wallet_variance", a.BasisKind)
	var postings int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_custody_effect_postings WHERE recorder_type<>'CryptoCustodyReceipt'").Scan(&postings))
	require.Zero(t, postings)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_custody_effect_postings(id,effect_id,occurrence_id,verification_id,generation,recorder_id,recorder_type,recorder_version)
		SELECT $1,o.effect_id,o.id,x.verification_id,1,$2,$3,1 FROM reg_crypto_accounting_effect_occurrences o JOIN reg_crypto_accounting_effect_evidence x ON x.occurrence_id=o.id`, id.New(), a.ID, "CryptoAdjustment")
	require.ErrorContains(t, err, "cannot claim a chain effect")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_custody_effect_postings(id,effect_id,occurrence_id,verification_id,generation,recorder_id,recorder_type,recorder_version)
		SELECT $1,o.effect_id,o.id,x.verification_id,1,$2,'OtherAdjustment',1 FROM reg_crypto_accounting_effect_occurrences o JOIN reg_crypto_accounting_effect_evidence x ON x.occurrence_id=o.id`, id.New(), a.ID)
	require.ErrorContains(t, err, "cannot claim a chain effect")
}

func TestAdjustmentBasisWalletVarianceRequiresBalancedSet(t *testing.T) {
	for _, fault := range []string{"valid", "missing_custody", "missing_variance", "wrong_variance", "missing_outbox"} {
		t.Run(fault, func(t *testing.T) {
			f := newPaymentAttributionFixture(t)
			a, err := adjustmentService().Propose(f.ctx, cryptoapp.ProposeFinancialAdjustmentCommand{Mode: "wallet_balance", WalletID: f.payment.WalletID, TokenID: f.payment.TokenID,
				Direction: "receipt", Amount: "7", MakerID: id.New(), ReasonCode: "wallet_other", Evidence: json.RawMessage(`{"description":"independent balance variance evidence"}`), IdempotencyKey: id.New().String()})
			require.NoError(t, err)
			err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
				q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
				if fault != "missing_custody" {
					if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
						VALUES($1,$2,'CryptoAdjustment',1,now(),'receipt',$3,$4,7)`, id.New(), a.ID, a.WalletID, a.TokenID); err != nil {
						return err
					}
				}
				if fault != "missing_variance" {
					kind := "surplus"
					if fault == "wrong_variance" {
						kind = "shortage"
					}
					if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_wallet_variance_movements(line_id,adjustment_id,wallet_id,token_id,variance_type,amount)
						VALUES($1,$2,$3,$4,$5,7)`, id.New(), a.ID, a.WalletID, a.TokenID, kind); err != nil {
						return err
					}
				}
				if _, err := q.Exec(ctx, "UPDATE doc_crypto_adjustments SET status='posted',checker_id=$2,decided_at=now(),decision_reason='checked variance accounting' WHERE id=$1", a.ID, id.New()); err != nil {
					return err
				}
				if fault == "missing_outbox" {
					return nil
				}
				return postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: "CryptoAdjustment", AggregateID: a.ID, EventType: "financial_correction_posted",
					Payload: map[string]any{"entityType": "CryptoAdjustment", "entityId": a.ID, "action": "financial_correction_posted", "basisKind": a.BasisKind, "postedVersion": 0}})
			})
			if fault == "valid" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, []string{"23", "0", "0"}, f.balances(t))
		})
	}
}

func TestAdjustmentBasisCannotApproveOldGeneration(t *testing.T) {
	f := settledAdjustmentFixture(t)
	a, err := adjustmentService().Propose(f.ctx, adjustmentCommand(f))
	require.NoError(t, err)
	event := f.request.Event
	event.BlockNumber++
	require.NoError(t, NewFinancialReorgRepo().RecordFinancialReorg(f.ctx, f.payment, event))
	f.payment.MarkUnposted()
	f.payment.Status = crypto_payment.PaymentStatusConfirming
	require.NoError(t, crypto_document_repo.NewCryptoPaymentRepo().Update(f.ctx, f.payment))
	require.NoError(t, f.post(f.payment, f.posting)) // The re-included transfer attributes the receipt again.
	_, err = f.pool.Exec(f.ctx, "UPDATE doc_crypto_payments SET status='settled' WHERE id=$1", f.payment.ID)
	require.NoError(t, err)
	_, err = adjustmentService().Approve(f.ctx, a.ID, id.New(), "stale generation decision")
	require.ErrorContains(t, err, "generation changed")
	require.Equal(t, []string{"0", "20", "3"}, f.balances(t))
}
