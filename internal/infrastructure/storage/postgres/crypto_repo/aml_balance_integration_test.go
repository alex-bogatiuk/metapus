//go:build integration

package crypto_repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
)

// amlAttributionSQL posts the attribution of a fixture payment to its
// receipt in one statement: suspense consumption, merchant credit and fee.
const amlAttributionSQL = `WITH p AS (SELECT * FROM doc_crypto_payments WHERE tx_hash=$1),
 s AS (INSERT INTO reg_crypto_receipt_suspense_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
  SELECT id,'CryptoPayment',posted_version,NOW(),'expense',receipt_id,token_id,amount FROM p RETURNING 1),
 m AS (INSERT INTO reg_crypto_merchant_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
  SELECT id,'CryptoPayment',posted_version,NOW(),'receipt',merchant_id,token_id,$2::numeric FROM p WHERE $2::numeric>0 RETURNING 1)
 INSERT INTO reg_crypto_fee_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount)
  SELECT gen_random_uuid_v7(),id,'CryptoPayment',posted_version,NOW(),'receipt',merchant_id,token_id,'processing',$3::numeric FROM p WHERE $3::numeric>0`

func (f amlRoutingFixture) attribute(t *testing.T, tx, net, fee string) {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, amlAttributionSQL, tx, net, fee)
	require.NoError(t, err)
}

func (f amlRoutingFixture) creditNet(t *testing.T, tx string) {
	t.Helper()
	f.attribute(t, tx, "90", "10")
}

func (f amlRoutingFixture) riskBalance(t *testing.T) aml.MerchantBalanceSnapshot {
	t.Helper()
	snapshot, err := cryptoapp.NewAMLBalanceService(f.repo).Get(f.ctx, f.input.Request.MerchantID)
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 1)
	r := snapshot.Items[0]
	require.Equal(t, r.Total.String(), r.Free.Add(r.Reserved).Add(r.TechnicalHold).String())
	total := r.Total.Sub(r.Total)
	for _, b := range r.Buckets {
		require.Equal(t, b.Total.String(), b.Free.Add(b.Reserved).Add(b.TechnicalHold).String())
		total = total.Add(b.Total)
	}
	require.Equal(t, r.Total.String(), total.String())
	return snapshot
}

func TestAMLBalanceUsesActualNetFeeAndCurrentRiskWithoutWritingMoney(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "net-incoming", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.creditNet(t, proof.Event.TxHash)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	snapshot := f.riskBalance(t)
	require.Equal(t, "90", snapshot.Items[0].Total.String())
	require.Equal(t, "90", snapshot.Items[0].Buckets[0].Free.String())
	require.True(t, snapshot.Items[0].AllocationComplete)
	require.Equal(t, snapshot.SnapshotID, f.riskBalance(t).SnapshotID, "unchanged evidence has a stable view identity")
	_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_risk_states SET expires_at=NOW()-interval '1 second' WHERE wallet_id=$1`, f.input.WalletID)
	require.NoError(t, err)
	expired := f.riskBalance(t)
	require.True(t, expired.Items[0].Buckets[0].Total.IsZero())
	require.Equal(t, "90", expired.Items[0].Buckets[3].Free.String(), "read cannot rely on a delayed TTL worker")
	require.NotEqual(t, snapshot.SnapshotID, expired.SnapshotID)
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "new adverse intelligence", "test-operator")
	require.NoError(t, err)
	f.complete(t, effectID, 9000, true)
	f.rebuild(t)
	high := f.riskBalance(t)
	require.Equal(t, "90", high.Items[0].Buckets[2].Total.String())
	require.True(t, high.Items[0].HasHighExposure)
	var physical, financial int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements`).Scan(&physical))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements`).Scan(&financial))
	require.Equal(t, 1, physical)
	require.Equal(t, 1, financial)
}

func TestAMLBalanceDebitFirstAndSourceReserveAreCountedOnce(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "reserved-incoming", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.creditNet(t, proof.Event.TxHash)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	requestID := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO doc_withdrawal_requests(id,merchant_id,token_id,amount,dest_address,status,posted,posted_version,risk_class)
 VALUES($1,$2,$3,20,'own-external-address','held',TRUE,1,'low')`, requestID, f.input.Request.MerchantID, f.input.Request.TokenID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_merchant_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
 VALUES($1,'WithdrawalRequest',1,NOW(),'expense',$2,$3,20)`, requestID, f.input.Request.MerchantID, f.input.Request.TokenID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_reservations(wallet_id,token_id,amount,document_type,document_id) VALUES($1,$2,20,'CryptoWithdrawal',$3)`, f.input.WalletID, f.input.Request.TokenID, requestID)
	require.NoError(t, err)
	r := f.riskBalance(t).Items[0]
	require.Equal(t, "70", r.LedgerBalance.String())
	require.Equal(t, "90", r.Total.String())
	require.Equal(t, "20", r.Reserved.String(), "source reserve is not added as another financial debit")
	require.Equal(t, "70", r.TechnicalHold.String())
	require.True(t, r.Free.IsZero())
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_withdrawal_requests SET status='failed' WHERE id=$1`, requestID)
	require.NoError(t, err)
	r = f.riskBalance(t).Items[0]
	require.True(t, r.Reserved.IsZero())
	require.Equal(t, "20", r.PendingRestoration.String())
	require.Equal(t, "90", r.TechnicalHold.String(), "terminal status alone cannot lose an uncompensated claim")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_merchant_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
 VALUES($1,'WithdrawalRequest',2,NOW(),'receipt',$2,$3,20)`, requestID, f.input.Request.MerchantID, f.input.Request.TokenID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_balance_reservations SET status='released' WHERE document_id=$1`, requestID)
	require.NoError(t, err)
	require.Equal(t, "90", f.riskBalance(t).Items[0].Free.String())
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_withdrawal_requests SET status='held' WHERE id=$1`, requestID)
	require.NoError(t, err)
	r = f.riskBalance(t).Items[0]
	require.Equal(t, "withdrawal_reconciliation_required", r.Reason)
	require.Equal(t, "90", r.TechnicalHold.String(), "a potentially executable request with a compensated debit cannot expose another free source")
}

func TestAMLBalanceLegacySpendsAndProjectionMismatchRequireReconciliation(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "allocation-incoming", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.creditNet(t, proof.Event.TxHash)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_merchant_balance_balances SET amount=80 WHERE merchant_id=$1 AND token_id=$2`, f.input.Request.MerchantID, f.input.Request.TokenID)
	require.NoError(t, err)
	r := f.riskBalance(t).Items[0]
	require.Equal(t, "90", r.Total.String(), "immutable financial movements remain authoritative")
	require.Equal(t, "ledger_reconciliation_required", r.Reason)
	require.Equal(t, "90", r.TechnicalHold.String())
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_merchant_balance_balances SET amount=90 WHERE merchant_id=$1 AND token_id=$2`, f.input.Request.MerchantID, f.input.Request.TokenID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
 VALUES($1,'CryptoSweep',1,NOW(),'expense',$2,$3,10)`, id.New(), f.input.WalletID, f.input.Request.TokenID)
	require.NoError(t, err)
	f.rebuild(t)
	r = f.riskBalance(t).Items[0]
	require.Equal(t, "source_allocation_incomplete", r.Reason)
	require.True(t, r.Free.IsZero(), "a partial spend cannot guess whether principal or fee moved")
	require.Equal(t, "90", r.TechnicalHold.String())
	foreign, err := cryptoapp.NewAMLBalanceService(f.repo).Get(f.ctx, id.New())
	require.NoError(t, err)
	require.Empty(t, foreign.Items)
	// A caller must not silently downgrade the required consistent snapshot.
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		_, _, err := f.repo.ReadMerchantRiskBalance(ctx, f.input.Request.MerchantID)
		return err
	})
	require.ErrorContains(t, err, "isolation")
	require.True(t, f.riskBalance(t).FreshUntil.After(time.Now()))
}
