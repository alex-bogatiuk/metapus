//go:build integration

package crypto_repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/stretchr/testify/require"
)

// Storage evidence isolates the new financial/AML contract. Dispatch permits
// and actual RPC quorum are exercised by their own driver/authorization suites.
func settleRiskLineV2(t *testing.T, f amlRoutingFixture, intent crypto.MoneyMovementIntent, line crypto.MoneyMovementSourceLine, native id.ID, success bool, afterDispatch func()) {
	t.Helper()
	doc, auth, start := id.New(), id.New(), id.New()
	hash := strings.ReplaceAll(doc.String()+doc.String(), "-", "")
	_, err := f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_withdrawals(id,accounting_model_version,merchant_id,token_id,source_wallet_id,dest_address,amount,status,basis_type,basis_id,tx_hash,posted,posted_version)
		VALUES($1,2,$2,$3,$4,$5,$6,'broadcast','CryptoAMLWithdrawalLine',$7,$8,true,1)`, doc, intent.MerchantID, intent.TokenID, line.WalletID, intent.DestinationAddress, line.AmountMinorUnits, line.LineID, hash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_withdrawal_lines SET withdrawal_id=$2,status='broadcast' WHERE id=$1`, line.LineID, doc)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,merchant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,
		source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,contract_address,amount_minor_units,basis_type,basis_id,expires_at,policy_version,authorization_jws,unsigned_fingerprint,unsigned_envelope,approval_bundle_digest,approval_bundle_json)
		VALUES($1,$1::uuid::text,$2,$3,'withdrawal',1,$4,'RISK-TEST','tron','nile',$5,$5,$6,$7,'trc20','test-contract',$8,'CryptoAMLWithdrawalLine',$9,now()+interval '1 hour','tron-v1','',$10,'{}','synthetic','{"synthetic":true}')`,
		auth, intent.TenantID, intent.MerchantID, f.input.Request.NetworkID, line.SourceAddress, line.SourceVaultKeyID, intent.DestinationAddress, line.AmountMinorUnits, line.LineID, "fp-"+hash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',signed_envelope='{"synthetic":true}',tx_hash=$2 WHERE id=$1`, auth, hash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_chain_effects(signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,token_id,contract_address_key,amount,tx_hash)
		VALUES($1,0,$2,'contract_log',$3,$4,$5,'test-contract',$6,$7)`, auth, f.input.Request.NetworkID, line.SourceAddress, intent.DestinationAddress, intent.TokenID, line.AmountMinorUnits, hash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_dispatch_starts(id,authorization_id,phase,snapshot) VALUES($1,$2,'broadcast','{"synthetic":true}')`, start, auth)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_first_dispatches(authorization_id,start_id,snapshot) VALUES($1,$2,'{"synthetic":true}')`, auth, start)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(accounting_model_version,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status,tx_hash)
		VALUES(1,$1,$2,$3,$4,'CryptoAMLWithdrawalLine',$5,'withdrawal',1,'native_burn',1,'broadcast',$6)`, f.input.Request.NetworkID, line.WalletID, native, intent.TokenID, line.LineID, hash)
	require.NoError(t, err)
	b := crypto.WithdrawalAccountingBasis{DocumentID: doc, RequestID: intent.BasisID, LineID: line.LineID, MerchantID: *intent.MerchantID, NetworkID: f.input.Request.NetworkID, TokenID: intent.TokenID, SourceWalletID: line.WalletID, TxHash: hash, Amount: line.AmountMinorUnits}
	receipt := crypto.ChainReceipt{NetworkID: b.NetworkID, TxHash: hash, Status: crypto.ReceiptReverted, BlockHash: strings.Repeat("a", 64), BlockHeight: 10, Confirmations: 20, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, ObservedAt: time.Now().UTC(), Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: native}}}
	if success {
		receipt.Status = crypto.ReceiptSucceeded
		proof := f.proof(intent.DestinationAddress, hash, 1)
		proof.Event.FromAddress = line.SourceAddress
		proof.Event.Amount = line.AmountMinorUnits
		proof.Event.Direction = crypto.TransferDirectionOutgoing
		proof.DecoderVersion = 2
		proof.Event.DecoderVersion = 2
		for i := range proof.Sources {
			proof.Sources[i].DecoderVersion = 2
			proof.Sources[i].SourceDigest = "outgoing-sweep"
		}
		require.NoError(t, proof.BindTRONAccountingIdentity())
		proofID, err := NewBlockIngestionRepo().SaveVerification(f.ctx, proof)
		require.NoError(t, err)
		b.VerificationID = &proofID
	}
	if afterDispatch != nil {
		afterDispatch()
	}
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		repo := NewWithdrawalCustodyRepo()
		proceed, err := repo.Prepare(ctx, b, receipt)
		if err != nil {
			return err
		}
		if !proceed {
			return nil
		}
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		settlement := id.New()
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_chain_settlements(id,network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,native_token_id,total_fee_minor,raw_receipt,observed_at)
			VALUES($1,$2,'CryptoAMLWithdrawalLine',$3,$4,$5,$6,10,'solidified',$7,0,'{}',$8)`, settlement, b.NetworkID, line.LineID, hash, receipt.Status, receipt.BlockHash, native, receipt.ObservedAt)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `UPDATE reg_crypto_fee_reservations SET status='settled',actual_fee_minor=0 WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=$1`, line.LineID)
		if err != nil {
			return err
		}
		if err := repo.RecordOutcome(ctx, b, receipt); err != nil {
			return err
		}
		state, result, reserve := "failed", "REVERTED", "released"
		if success {
			state, result, reserve = "confirmed", "SUCCESS", "consumed"
		}
		_, err = q.Exec(ctx, `UPDATE doc_crypto_withdrawals SET status=$2,settlement_status='recorded',chain_result=$3,network_fee=0,network_fee_token_id=$4 WHERE id=$1`, doc, state, result, native)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `UPDATE reg_crypto_balance_reservations SET status=$2 WHERE document_type='CryptoAMLWithdrawalLine' AND document_id=$1`, line.LineID, reserve)
		if err != nil {
			return err
		}
		var expense any
		if success {
			expense = withdrawalPrincipalLine(doc)
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_withdrawal_facts(line_id,wallet_id,token_id,amount,succeeded,settlement_id,expense_movement_id,refund_movement_id) VALUES($1,$2,$3,$4,$5,$6,$7,NULL)`, line.LineID, line.WalletID, intent.TokenID, line.AmountMinorUnits, success, settlement, expense)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `SELECT fn_crypto_aml_refresh_withdrawal($1)`, intent.BasisID)
		return err
	})
	require.NoError(t, err)
}

func TestWithdrawalV2AMLPartialSuccessAndRevertPreserveExactPrincipal(t *testing.T) {
	f := riskWithdrawalFixture(t, true)
	intent, err := reserveRiskWithdrawal(f, aml.Low, "150", false)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_withdrawal_requests SET status='approved' WHERE id=$1`, intent.BasisID)
	require.NoError(t, err)
	native := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,$1::uuid::text,'Native',$2,'TRX',6,'native')`, native, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,0,'checkpoint',0,'paused',2)`, f.input.Request.NetworkID)
	require.NoError(t, err)
	settleRiskLineV2(t, f, intent, intent.SourceLines[0], native, true, func() {
		// A later risk change stops new dispatch, but cannot erase an already
		// accepted signed transfer whose final proof is now available.
		_, riskErr := f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_risk_states SET risk_class='high' WHERE wallet_id=$1 AND token_id=$2`, intent.SourceLines[0].WalletID, intent.TokenID)
		require.NoError(t, riskErr)
	})
	f.rebuild(t)
	settleRiskLineV2(t, f, intent, intent.SourceLines[1], native, false, nil)
	f.rebuild(t)
	var state, merchant, principal string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM doc_withdrawal_requests WHERE id=$1`, intent.BasisID).Scan(&state))
	require.Equal(t, "partially_completed", state)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=$2`, intent.MerchantID, intent.TokenID).Scan(&merchant))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT sum(principal)::text FROM reg_crypto_aml_inventory_positions WHERE merchant_id=$1 AND token_id=$2`, intent.MerchantID, intent.TokenID).Scan(&principal))
	expected := types.NewCryptoAmountFromInt64(180).Sub(intent.SourceLines[0].AmountMinorUnits).String()
	require.Equal(t, []string{expected, expected}, []string{merchant, principal})
	var expenses, refunds int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements m JOIN doc_crypto_withdrawals w ON w.id=m.recorder_id WHERE w.accounting_model_version=2),
		(SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_type='AMLWithdrawalRevert')`).Scan(&expenses, &refunds))
	require.Equal(t, []int{1, 0}, []int{expenses, refunds})
}

func TestWithdrawalV2AMLCancellationPreservesCompletedPrincipal(t *testing.T) {
	f := riskWithdrawalFixture(t, true)
	intent, err := reserveRiskWithdrawal(f, aml.Low, "150", false)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_withdrawal_requests SET status='approved' WHERE id=$1`, intent.BasisID)
	require.NoError(t, err)
	native := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,$1::uuid::text,'Native',$2,'TRX',6,'native')`, native, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,0,'checkpoint',0,'paused',2)`, f.input.Request.NetworkID)
	require.NoError(t, err)
	settleRiskLineV2(t, f, intent, intent.SourceLines[0], native, true, nil)
	var refunded types.CryptoAmount
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		var cancelErr error
		refunded, cancelErr = f.repo.CancelAMLWithdrawalRemainder(ctx, *intent.MerchantID, intent.BasisID, "cancel unsigned remainder after one final transfer")
		return cancelErr
	})
	require.NoError(t, err)
	require.Equal(t, intent.SourceLines[1].AmountMinorUnits.String(), refunded.String())
	f.rebuild(t)
	var state, balance, principal string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM doc_withdrawal_requests WHERE id=$1`, intent.BasisID).Scan(&state))
	require.Equal(t, "partially_completed", state)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=$2`, intent.MerchantID, intent.TokenID).Scan(&balance))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT sum(principal)::text FROM reg_crypto_aml_inventory_positions WHERE merchant_id=$1 AND token_id=$2`, intent.MerchantID, intent.TokenID).Scan(&principal))
	expected := types.NewCryptoAmountFromInt64(180).Sub(intent.SourceLines[0].AmountMinorUnits).String()
	require.Equal(t, []string{expected, expected}, []string{balance, principal})
}
