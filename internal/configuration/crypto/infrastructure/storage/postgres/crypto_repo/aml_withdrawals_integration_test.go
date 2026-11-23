//go:build integration

package crypto_repo

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

func reserveRiskWithdrawal(f amlRoutingFixture, class aml.Class, amount string, accepted bool) (crypto.MoneyMovementIntent, error) {
	parent := id.New()
	merchant := f.input.Request.MerchantID
	intent := crypto.MoneyMovementIntent{SchemaVersion: crypto.MoneyMovementIntentV1, IntentID: id.New().String(), TenantID: id.New().String(), Purpose: crypto.SigningPurposeWithdrawal, BasisType: "WithdrawalRequest", BasisID: parent, MerchantID: &merchant, NetworkCode: "RISK-TEST", TokenID: f.input.Request.TokenID, TokenStandard: "trc20", ContractAddress: "test-contract", DestinationAddress: "own-external-wallet", AmountMinorUnits: types.MustRawAmount(amount), AddressID: id.New()}
	err := postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		enabled, err := f.repo.LockAMLWithdrawalScope(ctx, merchant, f.input.Request.NetworkID, f.input.Request.TokenID)
		if err != nil {
			return err
		}
		if !enabled {
			panic("test scope disabled")
		}
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		_, err = q.Exec(ctx, `INSERT INTO doc_withdrawal_requests(id,merchant_id,token_id,amount,dest_address,status,posted,posted_version,risk_class) VALUES($1,$2,$3,$4,'own-external-wallet','pending_signature',TRUE,1,$5)`, parent, merchant, f.input.Request.TokenID, intent.AmountMinorUnits, class)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount) VALUES($1,'WithdrawalRequest',1,NOW(),'expense',$2,$3,$4)`, parent, merchant, f.input.Request.TokenID, intent.AmountMinorUnits)
		if err != nil {
			return err
		}
		intent, err = f.repo.ReserveAMLWithdrawalSources(ctx, intent, aml.WithdrawalRequest{MerchantID: merchant, NetworkID: f.input.Request.NetworkID, TokenID: f.input.Request.TokenID, Class: class, Amount: intent.AmountMinorUnits, RiskAccepted: accepted})
		return err
	})
	return intent, err
}
func riskWithdrawalFixture(t *testing.T, two bool) amlRoutingFixture {
	t.Helper()
	f, collector, _ := readyAMLInventory(t)
	_, err := f.pool.Exec(f.ctx, `UPDATE cat_wallets SET chain_activation_status='active' WHERE id=$1`, f.input.WalletID)
	require.NoError(t, err)
	if two {
		proof := f.proof("TPartitionCo11ectorAddressbbbbbbbb", "second-withdrawal-source", 1)
		proofID, effect := f.save(t, proof)
		f.postPayment(t, proof, proofID, collector)
		f.creditNet(t, proof.Event.TxHash)
		f.complete(t, effect, 1000, true)
		f.rebuild(t)
	}
	return f
}
func TestAMLRiskWithdrawalSelectsSeveralExactSourcesAndDebitsOnce(t *testing.T) {
	f := riskWithdrawalFixture(t, true)
	intent, err := reserveRiskWithdrawal(f, aml.Low, "150", false)
	require.NoError(t, err)
	require.Len(t, intent.SourceLines, 2)
	require.NoError(t, intent.ValidateSources())
	var ledger, principal string
	var expenseCount int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=$2`, f.input.Request.MerchantID, f.input.Request.TokenID).Scan(&ledger))
	require.Equal(t, "30", ledger)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT SUM(principal)::text FROM reg_crypto_aml_inventory_positions WHERE merchant_id=$1 AND token_id=$2`, f.input.Request.MerchantID, f.input.Request.TokenID).Scan(&principal))
	require.Equal(t, "180", principal, "reservation never changes inventory or creates a second credit")
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$1 AND record_type='expense'`, intent.BasisID).Scan(&expenseCount))
	require.Equal(t, 1, expenseCount)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_withdrawal_lines SET amount=amount+1 WHERE request_id=$1`, intent.BasisID)
	require.Error(t, err)
	_, err = reserveRiskWithdrawal(f, aml.Medium, "1", false)
	require.Error(t, err, "another class cannot borrow the reserved low principal")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_withdrawal_plans`).Scan(&count))
	require.Equal(t, 1, count, "failed planning rolls back parent debit and reservations")
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_withdrawal_requests SET status='approved' WHERE id=$1`, intent.BasisID)
	require.NoError(t, err)
	tasks, err := f.repo.ClaimAMLWithdrawalLines(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	for _, task := range tasks {
		require.Equal(t, intent.BasisID, task.AMLParentID)
		require.Equal(t, "CryptoAMLWithdrawalLine", task.SigningBasis())
		require.False(t, id.IsNil(task.AMLLeaseToken))
		require.True(t, task.Amount.IsPositive())
		require.NotEmpty(t, task.AMLSource.VaultKeyID)
	}
	tasks, err = f.repo.ClaimAMLWithdrawalLines(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, tasks, "an unexpired execution lease cannot be claimed twice")
}
func TestAMLRiskWithdrawalConcurrentRequestsCannotOverspendPrincipal(t *testing.T) {
	f := riskWithdrawalFixture(t, false)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { _, errs[i] = reserveRiskWithdrawal(f, aml.Low, "60", false) })
	}
	wg.Wait()
	require.NotEqual(t, errs[0] == nil, errs[1] == nil)
	var ledger string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=$2`, f.input.Request.MerchantID, f.input.Request.TokenID).Scan(&ledger))
	require.Equal(t, "30", ledger)
}
func TestAMLRiskWithdrawalHighRequiresAcceptanceAndPreservesClass(t *testing.T) {
	f := riskWithdrawalFixture(t, false)
	var effect id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM reg_crypto_aml_effects WHERE wallet_id=$1 ORDER BY created_at LIMIT 1`, f.input.WalletID).Scan(&effect))
	_, err := f.repo.RequestAMLRescreen(f.ctx, effect, 1, "late high report", "test")
	require.NoError(t, err)
	f.complete(t, effect, 9000, true)
	f.rebuild(t)
	_, err = reserveRiskWithdrawal(f, aml.High, "20", false)
	require.Error(t, err)
	intent, err := reserveRiskWithdrawal(f, aml.High, "20", true)
	require.NoError(t, err)
	require.True(t, intent.RiskAccepted)
	require.Equal(t, "high", intent.RiskClass)
	_, err = reserveRiskWithdrawal(f, aml.Low, "1", false)
	require.Error(t, err)
}
func TestAMLRiskWithdrawalCancellationRefundsOnlyUnsignedRemainder(t *testing.T) {
	f := riskWithdrawalFixture(t, true)
	intent, err := reserveRiskWithdrawal(f, aml.Low, "150", false)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_withdrawal_requests SET status='approved' WHERE id=$1`, intent.BasisID)
	require.NoError(t, err)
	// One immutable authorization already exists: its outcome remains unknown.
	line := intent.SourceLines[0]
	auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: intent.TenantID, MerchantID: intent.MerchantID, Purpose: crypto.SigningPurposeWithdrawal, BasisType: "CryptoAMLWithdrawalLine", BasisID: line.LineID, Attempt: 1, Generation: 1, NetworkID: f.input.Request.NetworkID, NetworkCode: intent.NetworkCode, ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: "nile", SourceAddress: line.SourceAddress, FeePayerAddress: line.SourceAddress, VaultKeyID: line.SourceVaultKeyID, DestinationAddress: intent.DestinationAddress, AssetStandard: "trc20", ContractAddress: "test-contract", AmountMinorUnits: line.AmountMinorUnits, UnsignedFingerprint: "pending-" + line.LineID.String(), PolicyVersion: "tron-v1"}
	auth.ExpiresAt = time.Now().Add(time.Hour)
	// SQL requires approval bundle presence; this is synthetic storage evidence,
	// not an accepted signature or a call to a real signer.
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,merchant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,contract_address,amount_minor_units,basis_type,basis_id,expires_at,policy_version,authorization_jws,unsigned_fingerprint,unsigned_envelope,approval_bundle_digest,approval_bundle_json) VALUES($1::uuid,$1::text,$2,$3,'withdrawal',1,$4,$5,'tron','nile',$6,$6,$7,$8,'trc20','test-contract',$9,'CryptoAMLWithdrawalLine',$10,$11,'tron-v1','',$12,'{}','synthetic','{"synthetic":true}')`, auth.AuthorizationID, auth.TenantID, auth.MerchantID, auth.NetworkID, auth.NetworkCode, auth.SourceAddress, auth.VaultKeyID, auth.DestinationAddress, auth.AmountMinorUnits, auth.BasisID, auth.ExpiresAt, auth.UnsignedFingerprint)
	require.NoError(t, err)
	var refunded types.RawAmount
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		var e error
		refunded, e = f.repo.CancelAMLWithdrawalRemainder(ctx, f.input.Request.MerchantID, intent.BasisID, "cancel unsigned remainder")
		return e
	})
	require.NoError(t, err)
	require.Equal(t, intent.SourceLines[1].AmountMinorUnits.String(), refunded.String())
	var state string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_aml_withdrawal_lines WHERE id=$1`, line.LineID).Scan(&state))
	require.Equal(t, "reserved", state)
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		_, e := f.repo.CancelAMLWithdrawalRemainder(ctx, f.input.Request.MerchantID, intent.BasisID, "retry cancellation")
		return e
	})
	require.Error(t, err)
	var receipts int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$1 AND record_type='receipt'`, intent.BasisID).Scan(&receipts))
	require.Equal(t, 1, receipts)
}
