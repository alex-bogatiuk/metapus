//go:build integration

package crypto_repo

import (
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAMLExactSweepRenewalPreservesOriginalPayloadAndIsSingleUse(t *testing.T) {
	f, batch, native := renewableSweepFixture(t)
	request := batchAllocation(t, f, batch, "100")
	_, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=1000 WHERE network_id=$1`, request.NetworkID)
	require.NoError(t, err)
	auth, unsigned := f.allocationAuthorization(t, request)
	auth.ExpiresAt = time.Now().Add(-time.Minute)
	feeID := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(accounting_model_version,id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,unsigned_fingerprint) VALUES(1,$1,$2,$3,$4,$5,'CryptoSweepAttempt',$6,'sweep',1,'native_burn',1,$7)`, feeID, request.NetworkID, request.SourceWalletID, native, request.TokenID, request.SweepAttemptID, auth.UnsignedFingerprint)
	require.NoError(t, err)
	require.NoError(t, f.repo.EnsureAMLResourceBudget(f.ctx, feeID))
	row, err := newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_block_ingestion SET mode='hybrid',activated_at=NOW() WHERE network_id=$1`, request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_chain_watcher_state(network_id,watcher_key,last_timestamp,lease_owner,lease_expires_at) VALUES($1,'tron:solidified',EXTRACT(EPOCH FROM NOW())::bigint*1000,'test',NOW()+interval '1 hour'),($1,'tron:latest',EXTRACT(EPOCH FROM NOW())::bigint*1000,'test',NOW()+interval '1 hour')`, request.NetworkID)
	require.NoError(t, err)
	require.Error(t, beginRecordDispatch(f, row, "authorize"), "expired authorization cannot dispatch before manual grant")
	var original string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(a)::text FROM reg_crypto_signing_authorizations a WHERE id=$1`, row).Scan(&original))
	renewal, err := f.repo.AuthorizeExactSweepRenewal(f.ctx, row, "collector allowlist repaired; reuse exact original bytes", "operator")
	require.NoError(t, err)
	require.NoError(t, beginRecordDispatch(f, row, "authorize"))
	_, err = f.repo.AuthorizeExactSweepRenewal(f.ctx, row, "cannot repeatedly extend permission", "operator")
	require.Error(t, err)
	var after string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(a)::text FROM reg_crypto_signing_authorizations a WHERE id=$1`, row).Scan(&after))
	require.Equal(t, original, after)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_authorization_renewals SET authorization_jws='new-control-signature' WHERE id=$1`, renewal)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_authorization_renewals SET authorization_jws='replacement-signature' WHERE id=$1`, renewal)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_aml_authorization_renewals WHERE id=$1`, renewal)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_authorization_renewals SET expires_at=NOW()+interval '1 hour' WHERE id=$1`, renewal)
	require.Error(t, err)
	require.NoError(t, beginRecordDispatch(f, row, "sign"))
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_risk_states SET risk_class='high' WHERE wallet_id=$1 AND token_id=$2`, request.SourceWalletID, request.TokenID)
	require.NoError(t, err)
	require.Error(t, beginRecordDispatch(f, row, "sign"), "manual renewal never bypasses late risk changes")

}

// A real operating receipt and final internal funding supply gas to the token
// source. This keeps renewal tests inside the same native resource gates as runtime.
func renewableSweepFixture(t *testing.T) (amlRoutingFixture, aml.Batch, id.ID) {
	t.Helper()
	f, source, _ := nativeResourceFixture(t)
	native := f.input.Request.TokenID
	fee, funding := resourceFee(t, f, source, "20", "10")
	_, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	hash := signResourceFunding(t, f, source, fee, funding)
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, source).Scan(&address))
	proof := nativeResourceProof(t, f, address, hash, "20")
	proof.Event.FromAddress = f.input.Request.Address
	proof.Event.Direction = crypto.TransferDirectionInternal
	require.NoError(t, proof.BindTRONAccountingIdentity())
	f.save(t, proof)
	settleResourceEvidence(t, f, fee, funding, hash, "succeeded", "3", true)
	f.rebuild(t)
	var asset id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM cat_tokens WHERE network_id=$1 AND token_standard<>'native'`, f.input.Request.NetworkID).Scan(&asset))
	f.input.WalletID = source
	f.input.Request.TokenID = asset
	f.input.Request.Address = address
	f.input.Request.ContractAddress = "test-contract"
	f.input.Request.EventKind = "contract_log"
	f.baseline(t, source)
	receipt := f.proof(address, "renewable-token-deposit", 1)
	receipt.DecoderVersion, receipt.Event.DecoderVersion = 2, 2
	for i := range receipt.Sources {
		receipt.Sources[i].DecoderVersion = 2
		receipt.Sources[i].SourceDigest = "renewal-source"
	}
	receiptID, effect := f.save(t, receipt)
	f.postPayment(t, receipt, receiptID, source)
	f.creditNet(t, receipt.Event.TxHash)
	f.complete(t, effect, 1000, true)
	f.rebuild(t)
	collector := f.collector(t, f.input.Request.MerchantID, "renewal-collector")
	f.baseline(t, collector)
	f.rebuild(t)
	version, err := f.repo.ConfigureAMLBatchPolicy(f.ctx, aml.BatchPolicyRequest{MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID, TokenID: asset, Class: aml.Low, MaxGross: types.MustCryptoAmount("200"), MaxAgeSeconds: 600, Actor: "test"})
	require.NoError(t, err)
	batch, err := f.repo.OpenAMLBatch(f.ctx, aml.OpenBatchRequest{ID: id.New(), MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID, TokenID: asset, CollectorID: collector, Class: aml.Low, PolicyVersion: version, ScopeVersion: 1, Actor: "test"})
	require.NoError(t, err)
	return f, batch, native
}
