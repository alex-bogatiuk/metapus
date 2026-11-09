//go:build integration

package crypto_repo

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_merchant_credit"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_fee"
	"metapus/internal/domain/registers/crypto_merchant_balance"
	"metapus/internal/domain/registers/crypto_receipt_suspense"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

type fixedTariff struct{}

func (fixedTariff) Resolve(context.Context, id.ID, id.ID, crypto.FeeDirection) (crypto.EffectiveFee, error) {
	return crypto.EffectiveFee{FixedFee: types.MustCryptoAmount("1"), PercentBP: 100,
		MinFee: types.ZeroCryptoAmount(), MaxFee: types.ZeroCryptoAmount()}, nil
}

type merchantCreditFixture struct {
	receiptIntakeFixture
	credits  *cryptoapp.MerchantCreditService
	receipt  id.ID
	merchant id.ID
}

func newMerchantCreditFixture(t *testing.T, custodyOwner bool) merchantCreditFixture {
	t.Helper()
	f := newReceiptIntakeFixture(t, nil)
	merchant := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Credit merchant')`, merchant, "MC-"+id.Compact(merchant))
	require.NoError(t, err)
	if custodyOwner {
		_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET merchant_id=$2,custody_merchant_id=$2 WHERE id=$1`, f.request.Basis.WalletID, merchant)
		require.NoError(t, err)
	}
	doc, err := f.service.Record(f.ctx, f.request)
	require.NoError(t, err)
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&posting.CryptoReceiptSuspenseVisitor{})
	engine.AddVisitor(&posting.CryptoMerchantBalanceVisitor{})
	engine.AddVisitor(&posting.CryptoFeeVisitor{})
	engine.AddRecorder(posting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(register_repo.NewCryptoReceiptSuspenseRepo())))
	engine.AddRecorder(posting.NewCryptoMerchantBalanceRecorder(crypto_merchant_balance.NewService(register_repo.NewCryptoMerchantBalanceRepo())))
	engine.AddRecorder(posting.NewCryptoFeeRecorder(crypto_fee.NewService(register_repo.NewCryptoFeeRepo())))
	credits := cryptoapp.NewMerchantCreditService(postgres.MustGetTxManager(f.ctx), NewMerchantCreditRepo(), fixedTariff{},
		engine, numerator.New(), postgres.NewOutboxPublisher(), time.Now)
	return merchantCreditFixture{receiptIntakeFixture: f, credits: credits, receipt: doc.ID, merchant: merchant}
}

func (f merchantCreditFixture) amounts(t *testing.T) (suspense, merchant, fee string) {
	t.Helper()
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
		(SELECT amount::text FROM reg_crypto_receipt_suspense_balances WHERE receipt_id=$1),
		COALESCE((SELECT SUM(amount)::text FROM reg_crypto_merchant_balance_movements WHERE merchant_id=$2),'0'),
		COALESCE((SELECT SUM(amount)::text FROM reg_crypto_fee_movements WHERE merchant_id=$2),'0')`, f.receipt, f.merchant).
		Scan(&suspense, &merchant, &fee))
	return
}

func TestMerchantCreditBelowThresholdPostsAtOnce(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_receipt_credit_policies(token_id,dual_approval_threshold) VALUES($1,1000)`, f.request.Basis.TokenID)
	require.NoError(t, err)
	actor := id.New()
	doc, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: actor})
	require.NoError(t, err)
	require.Equal(t, crypto_merchant_credit.StatusPosted, doc.Status)
	require.False(t, doc.ApprovalRequired)
	gross := f.request.Basis.Amount
	fee, net := crypto.EffectiveFee{FixedFee: types.MustCryptoAmount("1"), PercentBP: 100}.Calculate(gross), types.ZeroCryptoAmount()
	net = gross.Sub(fee)
	suspense, merchant, charged := f.amounts(t)
	require.Equal(t, []string{"0", net.String(), fee.String()}, []string{suspense, merchant, charged})
	var custody int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1`, doc.ID).Scan(&custody))
	require.Zero(t, custody, "attribution never moves custody")
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: actor})
	require.ErrorContains(t, err, "no unattributed remainder")
}

func TestMerchantCreditAboveThresholdNeedsAnotherOperator(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	preparer := id.New()
	doc, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: preparer})
	require.NoError(t, err, "without a policy every credit needs a second operator")
	require.Equal(t, crypto_merchant_credit.StatusPrepared, doc.Status)
	suspense, merchant, _ := f.amounts(t)
	require.Equal(t, []string{f.request.Basis.Amount.String(), "0"}, []string{suspense, merchant})

	_, err = f.credits.Approve(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, ExpectedVersion: doc.Version, Actor: preparer})
	require.True(t, apperror.IsForbidden(err), "%v", err)
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.Error(t, err, "one open credit per receipt")

	approved, err := f.credits.Approve(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, ExpectedVersion: doc.Version, Actor: id.New()})
	require.NoError(t, err)
	require.Equal(t, crypto_merchant_credit.StatusPosted, approved.Status)
	suspense, merchant, _ = f.amounts(t)
	require.Equal(t, "0", suspense)
	require.Equal(t, approved.NetAmount.String(), merchant)

	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_merchant_credits SET amount=amount+1 WHERE id=$1`, doc.ID)
	require.ErrorContains(t, err, "single decision")
}

func TestMerchantCreditRejectionKeepsSuspenseAndAllowsNewCredit(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	doc, err := f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
	_, err = f.credits.Reject(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, Actor: id.New()})
	require.Error(t, err, "rejection needs a reason")
	rejected, err := f.credits.Reject(f.ctx, cryptoapp.DecideMerchantCreditCommand{CreditID: doc.ID, Actor: id.New(), Reason: "sender is not the merchant's customer"})
	require.NoError(t, err)
	require.Equal(t, crypto_merchant_credit.StatusRejected, rejected.Status)
	suspense, merchant, _ := f.amounts(t)
	require.Equal(t, []string{f.request.Basis.Amount.String(), "0"}, []string{suspense, merchant})
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
}

func TestMerchantCreditRespectsCustodyOwner(t *testing.T) {
	f := newMerchantCreditFixture(t, true)
	other := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Other merchant')`, other, "MO-"+id.Compact(other))
	require.NoError(t, err)
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: other, Actor: id.New()})
	require.True(t, apperror.IsForbidden(err), "%v", err)
	_, err = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
	require.NoError(t, err)
}

func TestMerchantCreditConcurrentPreparationCreditsOnce(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_receipt_credit_policies(token_id,dual_approval_threshold) VALUES($1,$2)`,
		f.request.Basis.TokenID, f.request.Basis.Amount)
	require.NoError(t, err)
	const workers = 6
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.credits.Prepare(f.ctx, cryptoapp.PrepareMerchantCreditCommand{ReceiptID: f.receipt, MerchantID: f.merchant, Actor: id.New()})
		}()
	}
	wg.Wait()
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	require.Equal(t, 1, succeeded)
	suspense, _, _ := f.amounts(t)
	require.Equal(t, "0", suspense)
}

func TestSuspenseCannotBeConsumedWithoutItsCredit(t *testing.T) {
	f := newMerchantCreditFixture(t, false)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_receipt_suspense_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
		VALUES($1,'CryptoMerchantCredit',1,NOW(),'expense',$2,$3,1)`, id.New(), f.receipt, f.request.Basis.TokenID)
	require.ErrorContains(t, err, "exact merchant credit")
}
