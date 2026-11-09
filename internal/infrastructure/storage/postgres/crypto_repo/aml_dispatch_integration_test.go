//go:build integration

package crypto_repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
)

func beginRecordDispatch(f amlRoutingFixture, recordID id.ID, phase string) error {
	var authorization string
	if err := f.pool.QueryRow(f.ctx, `SELECT authorization_id FROM reg_crypto_signing_authorizations WHERE id=$1`, recordID).Scan(&authorization); err != nil {
		return err
	}
	return BeginAMLDispatch(f.ctx, authorization, phase)
}

func TestAMLDispatchRequiresCommitBeforeExternalCall(t *testing.T) {
	f, _, _, auth := resourceActivation(t)
	require.Error(t, postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error { return BeginAMLDispatch(ctx, auth.String(), "authorize") }))
}

func resourceActivation(t *testing.T) (amlRoutingFixture, id.ID, id.ID, id.ID) {
	t.Helper()
	f, target, _ := nativeResourceFixture(t)
	fee, task := id.New(), id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_account_activations(id,wallet_id,network_id) VALUES($1,$2,$3)`, task, target, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(accounting_model_version,id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor) VALUES(1,$1,$2,$3,$4,$4,'CryptoAccountActivationTask',$5,'activation',1,'native_burn',10)`, fee, f.input.Request.NetworkID, f.input.WalletID, f.input.Request.TokenID, task)
	require.NoError(t, err)
	require.NoError(t, f.repo.EnsureAMLResourceBudget(f.ctx, fee))
	var dest string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, target).Scan(&dest))
	auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(), MerchantID: &f.input.Request.MerchantID, Purpose: crypto.SigningPurposeActivation, BasisType: "CryptoAccountActivationTask", BasisID: task, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour), NetworkID: f.input.Request.NetworkID, NetworkCode: "RESOURCE-TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: "nile", SourceAddress: f.input.Request.Address, FeePayerAddress: f.input.Request.Address, VaultKeyID: "resource-key", DestinationAddress: dest, AssetStandard: "native", MaxNetworkFeeMinor: types.MustCryptoAmount("10"), UnsignedFingerprint: "activation-" + task.String(), PolicyVersion: "tron-v1"}
	unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{"synthetic":true}`)}
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET unsigned_fingerprint=$2 WHERE id=$1`, fee, auth.UnsignedFingerprint)
	require.NoError(t, err)
	authID, err := newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_network_policies(network_id,enabled,per_transaction_max_fee_minor,daily_network_fee_budget_minor) VALUES($1,TRUE,10,1000) ON CONFLICT(network_id) DO UPDATE SET enabled=TRUE,per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=1000`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_block_ingestion SET mode='hybrid',activated_at=NOW() WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_chain_watcher_state(network_id,watcher_key,last_timestamp,lease_owner,lease_expires_at) VALUES($1,'tron:solidified',EXTRACT(EPOCH FROM NOW())::bigint*1000,'test',NOW()+interval '1 hour'),($1,'tron:latest',EXTRACT(EPOCH FROM NOW())::bigint*1000,'test',NOW()+interval '1 hour')`, f.input.Request.NetworkID)
	require.NoError(t, err)
	return f, target, fee, authID
}

func TestAMLDispatchStartRechecksRiskAndPreservesFirstSnapshot(t *testing.T) {
	f, _, _, auth := resourceActivation(t)
	require.NoError(t, beginRecordDispatch(f, auth, "authorize"))
	require.NoError(t, beginRecordDispatch(f, auth, "sign"))
	signed, err := json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, TxHash: "activation-dispatch", Payload: []byte(`{"synthetic":true}`)})
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',signed_envelope=$2::jsonb,tx_hash='activation-dispatch' WHERE id=$1`, auth, string(signed))
	require.NoError(t, err)
	require.NoError(t, beginRecordDispatch(f, auth, "broadcast"))
	require.NoError(t, beginRecordDispatch(f, auth, "broadcast"), "each retry records another start for the same outflow")
	var original string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT snapshot::text FROM reg_crypto_aml_first_dispatches WHERE authorization_id=$1`, auth).Scan(&original))
	_, err = f.pool.Exec(f.ctx, `SELECT fn_crypto_aml_invalidate_wallet($1,$2,'late-risk')`, f.input.WalletID, f.input.Request.TokenID)
	require.NoError(t, err)
	require.Error(t, beginRecordDispatch(f, auth, "sign"))
	require.Error(t, beginRecordDispatch(f, auth, "broadcast"))
	f.rebuild(t)
	require.Error(t, beginRecordDispatch(f, auth, "sign"), "a refreshed revision never permits a new signature")
	require.NoError(t, beginRecordDispatch(f, auth, "broadcast"), "same-class recovery revalidates exact saved bytes")
	var current string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT snapshot::text FROM reg_crypto_aml_first_dispatches WHERE authorization_id=$1`, auth).Scan(&current))
	require.Equal(t, original, current)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_dispatch_starts WHERE authorization_id=$1`, auth).Scan(&count))
	require.Equal(t, 5, count)
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_aml_dispatch_starts WHERE authorization_id=$1`, auth)
	require.Error(t, err)
}

func TestAMLDispatchStopsOnIngestionLagAndUnprocessedInbox(t *testing.T) {
	f, _, _, auth := resourceActivation(t)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_chain_watcher_state SET updated_at=NOW()-interval '3 minutes' WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	require.Error(t, beginRecordDispatch(f, auth, "authorize"))
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_chain_watcher_state SET updated_at=NOW() WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_chain_observations(network_id,source_stream,tx_hash,event_kind,event_index,block_number,block_hash,payload_hash,payload) VALUES($1,'test','unprocessed','native_value',0,11,'block','payload','{}')`, f.input.Request.NetworkID)
	require.NoError(t, err)
	require.Error(t, beginRecordDispatch(f, auth, "authorize"))
}

func TestAMLActivationSettlesOperatingCostWithoutPrincipalTransfer(t *testing.T) {
	for _, status := range []string{"succeeded", "reverted"} {
		t.Run(status, func(t *testing.T) {
			f, target, fee, auth := resourceActivation(t)
			var task id.ID
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT basis_id FROM reg_crypto_signing_authorizations WHERE id=$1`, auth).Scan(&task))
			operation, settlement, movement := id.New(), id.New(), id.New()
			_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET status='broadcast',tx_hash='activation-settle' WHERE id=$1`, fee)
			require.NoError(t, err)
			_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_chain_settlements(id,network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,native_token_id,total_fee_minor,raw_receipt,observed_at) VALUES($1,$2,'CryptoNetworkOperation',$3,'activation-settle',$4,repeat('a',64),10,'solidified',$5,3,'{}',NOW())`, settlement, f.input.Request.NetworkID, operation, status, f.input.Request.TokenID)
			require.NoError(t, err)
			_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_network_operations(id,basis_type,basis_id,operation_type,status,network_id,target_wallet_id,target_address,source_wallet_id,cost_token_id,cost_amount,tx_hash,chain_settlement_id,finalized_at,posted,posted_version) SELECT $1,'CryptoAccountActivationTask',$2,'wallet_activation',$3,$4,id,address,$5,$6,3,'activation-settle',$7,NOW(),TRUE,1 FROM cat_wallets WHERE id=$8`, operation, task, status, f.input.Request.NetworkID, f.input.WalletID, f.input.Request.TokenID, settlement, target)
			require.NoError(t, err)
			_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_account_activations SET network_operation_id=$2 WHERE id=$1`, task, operation)
			require.NoError(t, err)
			_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,$2,'CryptoNetworkOperation',1,NOW(),'expense',$3,$4,3)`, movement, operation, f.input.WalletID, f.input.Request.TokenID)
			require.NoError(t, err)
			_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET status='settled',actual_fee_minor=3 WHERE id=$1`, fee)
			require.NoError(t, err)
			f.rebuild(t)
			require.Equal(t, []string{"97", "0"}, resourceAmounts(t, f, target))
			var principal string
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT principal::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1 AND token_id=$2`, f.input.WalletID, f.input.Request.TokenID).Scan(&principal))
			require.Equal(t, "0", principal)
		})
	}
}

func TestAMLDispatchRequiresLiveWatcherAndRecentChainTimestamp(t *testing.T) {
	f, _, _, auth := resourceActivation(t)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_chain_watcher_state SET last_timestamp=EXTRACT(EPOCH FROM NOW()-interval '1 hour')::bigint*1000 WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	require.Error(t, beginRecordDispatch(f, auth, "authorize"))
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_chain_watcher_state SET last_timestamp=EXTRACT(EPOCH FROM NOW())::bigint*1000,lease_expires_at=NOW()-interval '1 second' WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	require.Error(t, beginRecordDispatch(f, auth, "authorize"))
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_chain_watcher_state SET lease_expires_at=NOW()+interval '1 minute' WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	require.NoError(t, beginRecordDispatch(f, auth, "authorize"))
}

// Exercise the real dispatch function with both principal and operating journals;
// the fixture signs/broadcasts nothing outside the disposable SQL database.
func TestAMLDispatchWithdrawalPreservesExplicitPrincipalClass(t *testing.T) {
	for _, class := range []aml.Class{aml.Low, aml.High, aml.Unknown} {
		t.Run(string(class), func(t *testing.T) {
			f, target, _, _ := resourceActivation(t)
			fee, funding := resourceFee(t, f, target, "20", "10")
			_, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
			require.NoError(t, err)
			txHash := signResourceFunding(t, f, target, fee, funding)
			var destination string
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, target).Scan(&destination))
			proof := nativeResourceProof(t, f, destination, txHash, "20")
			proof.Event.FromAddress, proof.Event.Direction = f.input.Request.Address, crypto.TransferDirectionInternal
			require.NoError(t, proof.BindTRONAccountingIdentity())
			f.save(t, proof)
			settleResourceEvidence(t, f, fee, funding, txHash, "succeeded", "3", true)
			f.rebuild(t)
			require.Equal(t, []string{"77", "20"}, resourceAmounts(t, f, target))
			principal := f
			principal.input.WalletID = target
			principal.input.Request.Address = destination
			principal.input.Request.ContractAddress = "test-contract"
			principal.input.MappingVersion = "test-mapping-v1"
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM cat_tokens WHERE network_id=$1 AND token_standard='trc20'`, f.input.Request.NetworkID).Scan(&principal.input.Request.TokenID))
			principal.baseline(t, target)
			incoming := principal.proof(destination, "withdrawal-with-gas", 1)
			incoming.DecoderVersion, incoming.Event.DecoderVersion = 2, 2
			for i := range incoming.Sources {
				incoming.Sources[i].DecoderVersion, incoming.Sources[i].SourceDigest = 2, "synthetic-v2-contract-source"
			}
			proofID, effect := principal.save(t, incoming)
			principal.postPayment(t, incoming, proofID, target)
			principal.creditNet(t, incoming.Event.TxHash)
			score := aml.ScoreBP(1000)
			if class == aml.High {
				score = 9000
			}
			principal.complete(t, effect, score, class != aml.Unknown)
			principal.rebuild(t)
			_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET chain_activation_status='active' WHERE id=$1`, target)
			require.NoError(t, err)
			intent, err := reserveRiskWithdrawal(principal, class, "20", class != aml.Low)
			require.NoError(t, err)
			_, err = f.pool.Exec(f.ctx, `UPDATE doc_withdrawal_requests SET status='approved' WHERE id=$1`, intent.BasisID)
			require.NoError(t, err)
			require.Len(t, intent.SourceLines, 1)
			line := intent.SourceLines[0]
			fee = id.New()
			fingerprint := "withdrawal-" + line.LineID.String()
			_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(accounting_model_version,id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,unsigned_fingerprint) VALUES(1,$1,$2,$3,$4,$5,'CryptoAMLWithdrawalLine',$6,'withdrawal',1,'native_burn',10,$7)`, fee, f.input.Request.NetworkID, target, f.input.Request.TokenID, principal.input.Request.TokenID, line.LineID, fingerprint)
			require.NoError(t, err)
			require.NoError(t, f.repo.EnsureAMLResourceBudget(f.ctx, fee))
			auth := id.New()
			_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_signing_authorizations(authorization_id,tenant_id,merchant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,contract_address,amount_minor_units,max_network_fee_minor,basis_type,basis_id,expires_at,policy_version,authorization_jws,unsigned_fingerprint,unsigned_envelope,approval_bundle_digest,approval_bundle_json) VALUES($1::text,$2,$3,'withdrawal',1,$4,'SYNTHETIC','tron','nile',$5,$5,$6,$7,'trc20','test-contract',20,10,'CryptoAMLWithdrawalLine',$8,NOW()+interval '1 hour','tron-v1','synthetic',$9,'{}','synthetic','{"synthetic":true}')`, auth.String(), intent.TenantID, intent.MerchantID, f.input.Request.NetworkID, destination, line.SourceVaultKeyID, intent.DestinationAddress, line.LineID, fingerprint)
			require.NoError(t, err)
			require.NoError(t, BeginAMLDispatch(f.ctx, auth.String(), "authorize"))
			require.NoError(t, BeginAMLDispatch(f.ctx, auth.String(), "sign"))
			signed, err := json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: fingerprint, TxHash: "withdrawal-dispatch", Payload: []byte(`{"synthetic":true}`)})
			require.NoError(t, err)
			_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',signed_envelope=$2::jsonb,tx_hash='withdrawal-dispatch' WHERE authorization_id=$1`, auth.String(), string(signed))
			require.NoError(t, err)
			require.NoError(t, BeginAMLDispatch(f.ctx, auth.String(), "broadcast"))
			var snapshotClass, source string
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT snapshot->'withdrawalPlan'->>'risk_class',snapshot->'withdrawalLine'->>'id' FROM reg_crypto_aml_first_dispatches d JOIN reg_crypto_signing_authorizations a ON a.id=d.authorization_id WHERE a.authorization_id=$1`, auth.String()).Scan(&snapshotClass, &source))
			require.Equal(t, string(class), snapshotClass)
			require.Equal(t, line.LineID.String(), source)
		})
	}
}
