//go:build integration

package crypto_repo

import (
	"context"
	"errors"
	"strings"
	"sync"
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

func readyAMLInventory(t *testing.T) (amlRoutingFixture, id.ID, id.ID) {
	t.Helper()
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "partition-deposit", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.creditNet(t, proof.Event.TxHash)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	collector := f.collector(t, f.input.Request.MerchantID, "TPartitionCo11ectorAddressbbbbbbbb")
	f.baseline(t, collector)
	f.rebuild(t)
	return f, collector, effectID
}

func (f amlRoutingFixture) allocationRequest(t *testing.T, destination id.ID, amount string) aml.InternalAllocationRequest {
	t.Helper()
	r := aml.InternalAllocationRequest{SweepAttemptID: id.New(), MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID,
		TokenID: f.input.Request.TokenID, SourceWalletID: f.input.WalletID, DestinationID: destination, Amount: types.MustCryptoAmount(amount), Class: aml.Low}
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT p.revision,r.revision,s.version FROM reg_crypto_aml_inventory_positions p JOIN reg_crypto_wallet_risk_states r ON r.wallet_id=p.wallet_id AND r.token_id=p.token_id
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=p.token_id WHERE p.wallet_id=$1 AND p.token_id=$2`, r.SourceWalletID, r.TokenID).Scan(&r.SourceRevision, &r.RiskRevision, &r.ScopeVersion))
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at)
 SELECT $1,$2,$3,$4,$5,$6,address,$7,'claimed',NOW()+interval '1 hour' FROM cat_wallets WHERE id=$5`, r.SweepAttemptID, r.SourceWalletID, r.TokenID, r.NetworkID, r.DestinationID, f.input.Request.Address, r.Amount)
	require.NoError(t, err)
	return r
}

func (f amlRoutingFixture) allocationAuthorization(t *testing.T, request aml.InternalAllocationRequest) (crypto.SigningAuthorization, crypto.UnsignedTxEnvelope) {
	t.Helper()
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, request.DestinationID).Scan(&address))
	auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(), MerchantID: &request.MerchantID,
		Purpose: crypto.SigningPurposeSweep, BasisType: "CryptoSweepAttempt", BasisID: request.SweepAttemptID, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour),
		NetworkID: request.NetworkID, NetworkCode: "RISK-TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: "nile", SourceAddress: f.input.Request.Address, FeePayerAddress: f.input.Request.Address,
		VaultKeyID: "test-key", DestinationAddress: address, AssetStandard: "trc20", ContractAddress: "test-contract", AmountMinorUnits: request.Amount,
		MaxNetworkFeeMinor: types.MustCryptoAmount("1"), UnsignedFingerprint: "partition-" + request.SweepAttemptID.String(), PolicyVersion: "tron-v1"}
	unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Generation: 1, Payload: []byte(`{"synthetic":true}`)}
	return auth, unsigned
}

func (f amlRoutingFixture) proveAllocatedSweep(t *testing.T, request aml.InternalAllocationRequest, post bool) id.ID {
	t.Helper()
	auth, unsigned := f.allocationAuthorization(t, request)
	authID, err := newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.NoError(t, err)
	txHash := strings.ReplaceAll(request.SweepAttemptID.String()+request.SweepAttemptID.String(), "-", "")
	signed, err := json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Generation: 1, TxHash: txHash, Payload: []byte(`{"synthetic":true}`)})
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',tx_hash=$2,signed_envelope=$3::jsonb WHERE id=$1`, authID, txHash, string(signed))
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_chain_effects(signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,token_id,contract_address_key,amount,tx_hash)
 VALUES($1,0,$2,'contract_log',$3,$4,$5,'test-contract',$6,$7)`, authID, request.NetworkID, auth.SourceAddress, auth.DestinationAddress, request.TokenID, request.Amount, txHash)
	require.NoError(t, err)
	proof := f.proof(auth.DestinationAddress, txHash, 1)
	proof.Event.FromAddress, proof.Event.Amount, proof.Event.Direction = auth.SourceAddress, request.Amount, crypto.TransferDirectionInternal
	f.saveTRONAccountingProof(t, proof)
	sweepID := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_sweeps(id,token_id,hot_wallet_id,total_amount,posted,posted_version,status) VALUES($1,$2,$3,$4,TRUE,1,'broadcast')`, sweepID, request.TokenID, request.DestinationID, request.Amount)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_sweeps__lines(line_id,document_id,wallet_id,amount,tx_hash) VALUES($1,$2,$3,$4,$5)`, request.SweepAttemptID, sweepID, request.SourceWalletID, request.Amount, txHash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_sweep_attempts SET sweep_id=$2,status='broadcast' WHERE id=$1`, request.SweepAttemptID, sweepID)
	require.NoError(t, err)
	if post {
		f.postAllocatedSweep(t, request, sweepID)
	}
	return sweepID
}

// postAllocatedSweep settles the sweep line from its final proof: the
// independent network fee first, then the per-line principal recognition.
func (f amlRoutingFixture) postAllocatedSweep(t *testing.T, request aml.InternalAllocationRequest, sweepID id.ID) {
	t.Helper()
	f.settleSweepLine(t, sweepID, request.SweepAttemptID, request.SourceWalletID, request.DestinationID, request.TokenID, request.NetworkID, request.Amount)
}

func (f amlRoutingFixture) settleSweepLine(t *testing.T, sweepID, lineID, source, destination, token, network id.ID, amount types.CryptoAmount) {
	t.Helper()
	var txHash string
	var proofID id.ID
	var height int64
	var block string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT l.tx_hash,v.id,v.block_number,v.block_hash FROM doc_crypto_sweeps__lines l
		JOIN reg_crypto_transfer_verifications v ON v.tx_hash=l.tx_hash WHERE l.line_id=$1`, lineID).Scan(&txHash, &proofID, &height, &block))
	native := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,token_standard,contract_address,decimal_places) VALUES($1,$1::uuid::text,'Native',$2,'TRX','native','',6)`, native, network)
	require.NoError(t, err)
	fee := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,tx_hash,status)
		VALUES($1,$2,$3,$4,$5,0,'CryptoSweepAttempt',$6,'sweep',1,'native_burn',1,$7,'broadcast')`, fee, network, source, native, token, lineID, txHash)
	require.NoError(t, err)
	receipt := crypto.ChainReceipt{NetworkID: network, TxHash: txHash, Status: crypto.ReceiptSucceeded, BlockHeight: height, BlockHash: block, Confirmations: 20,
		Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, ObservedAt: time.Now().UTC(), Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: native}}}
	basis := crypto.SweepAccountingBasis{DocumentID: sweepID, LineID: lineID, NetworkID: network, TokenID: token,
		SourceWalletID: source, DestinationWalletID: destination, Amount: amount, TxHash: txHash, VerificationID: &proofID}
	require.NoError(t, postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		repo := NewSweepCustodyRepo()
		proceed, err := repo.Prepare(ctx, basis, receipt)
		if err != nil || !proceed {
			return err
		}
		if err := recognizeTestNetworkFee(ctx, fee, receipt); err != nil {
			return err
		}
		if err := repo.RecordPrincipal(ctx, basis, receipt); err != nil {
			return err
		}
		_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "UPDATE doc_crypto_sweeps__lines SET settlement_status='recorded',chain_result='SUCCESS',confirmed=true,network_fee=0,network_fee_token_id=$2 WHERE line_id=$1", lineID, native)
		return err
	}))
}

func TestAMLInventoryPartialTransferCarriesExactPrincipalAndFeeWithoutCredit(t *testing.T) {
	f, destination, effectID := readyAMLInventory(t)
	request := f.allocationRequest(t, destination, "50")
	plan, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "45", plan.Partition.Principal.String())
	require.Equal(t, "5", plan.Partition.ServiceFee.String())
	require.Equal(t, "90", f.riskBalance(t).Items[0].TechnicalHold.String())
	sweepID := f.proveAllocatedSweep(t, request, false)
	f.rebuild(t)
	var facts int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_inventory_facts`).Scan(&facts))
	require.Equal(t, 1, facts, "chain proof without physical posting cannot apply another component movement")
	f.postAllocatedSweep(t, request, sweepID)
	f.rebuild(t)
	var sourceNet, sourceFee, destinationNet, destinationFee string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT principal::text,service_fee::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1`, request.SourceWalletID).Scan(&sourceNet, &sourceFee))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT principal::text,service_fee::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1`, destination).Scan(&destinationNet, &destinationFee))
	require.Equal(t, []string{"45", "5", "45", "5"}, []string{sourceNet, sourceFee, destinationNet, destinationFee})
	r := f.riskBalance(t).Items[0]
	require.Equal(t, "90", r.Free.String())
	require.True(t, r.AllocationComplete)
	var credits int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements`).Scan(&credits))
	require.Equal(t, 1, credits, "internal allocation cannot create another merchant receipt")
	f.rebuild(t)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_inventory_facts`).Scan(&facts))
	require.Equal(t, 2, facts, "replay cannot carry the same principal/fee twice")
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "late adverse intelligence", "operator")
	require.NoError(t, err)
	f.complete(t, effectID, 9000, true)
	f.rebuild(t)
	r = f.riskBalance(t).Items[0]
	require.Equal(t, "90", r.Buckets[2].Total.String(), "both source remainder and collector inherit late high")
	require.Equal(t, "90", r.Total.String())
	require.Error(t, f.repo.CancelAMLInternalAllocation(f.ctx, plan.ID, "already applied", "operator"))
}

func TestAMLInventoryInFlightSplitDoesNotChangeAfterAnotherPayment(t *testing.T) {
	f, destination, _ := readyAMLInventory(t)
	request := f.allocationRequest(t, destination, "50")
	_, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	sweepID := f.proveAllocatedSweep(t, request, false)
	proof := f.proof(f.input.Request.Address, strings.Repeat("ab", 32), 1)
	proofID, effectID := f.save(t, proof)
	f.postPaymentFee(t, proof, proofID, f.input.WalletID, "50")
	f.attribute(t, proof.Event.TxHash, "50", "50")
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	f.postAllocatedSweep(t, request, sweepID)
	f.rebuild(t)
	var sourceNet, sourceFee, destinationNet, destinationFee string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT principal::text,service_fee::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1`, request.SourceWalletID).Scan(&sourceNet, &sourceFee))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT principal::text,service_fee::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1`, destination).Scan(&destinationNet, &destinationFee))
	require.Equal(t, []string{"95", "55", "45", "5"}, []string{sourceNet, sourceFee, destinationNet, destinationFee}, "new receipts cannot rewrite the ownership split of an already signed transfer")
	require.Equal(t, "140", f.riskBalance(t).Items[0].Free.String())
}

func TestAMLInventoryConcurrentReservationIsIdempotentAndExpiredAttemptDoesNotRelease(t *testing.T) {
	f, destination, _ := readyAMLInventory(t)
	request := f.allocationRequest(t, destination, "60")
	var workers sync.WaitGroup
	results := make(chan aml.InternalAllocation, 6)
	errorsFound := make(chan error, 6)
	for range 6 {
		workers.Go(func() {
			result, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
			results <- result
			errorsFound <- err
		})
	}
	workers.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}
	var planID id.ID
	for result := range results {
		if id.IsNil(planID) {
			planID = result.ID
		}
		require.Equal(t, planID, result.ID)
	}
	changed := request
	changed.Amount = types.MustCryptoAmount("59")
	_, err := f.repo.ReserveAMLInternalAllocation(f.ctx, changed)
	require.ErrorContains(t, err, "request changed")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_sweep_attempts SET status='expired' WHERE id=$1`, request.SweepAttemptID)
	require.NoError(t, err)
	second := f.allocationRequest(t, destination, "50")
	_, err = f.repo.ReserveAMLInternalAllocation(f.ctx, second)
	require.ErrorContains(t, err, "insufficient allocated inventory")
	require.NoError(t, f.repo.CancelAMLInternalAllocation(f.ctx, planID, "unsigned reservation superseded", "operator"))
	require.NoError(t, f.repo.CancelAMLInternalAllocation(f.ctx, planID, "unsigned reservation superseded", "operator"))
	require.ErrorContains(t, f.repo.CancelAMLInternalAllocation(f.ctx, planID, "different cancellation reason", "operator"), "original reason or actor")
	require.ErrorContains(t, f.repo.CancelAMLInternalAllocation(f.ctx, planID, "unsigned reservation superseded", "another operator"), "original reason or actor")
	require.Equal(t, "90", f.riskBalance(t).Items[0].Free.String())
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_allocation_cancellations SET reason='rewrite' WHERE allocation_id=$1`, planID)
	require.ErrorContains(t, err, "append-only")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_sweep_attempts SET amount=59 WHERE id=$1`, request.SweepAttemptID)
	require.ErrorContains(t, err, "identity is immutable")
}

func TestAMLInventoryCannotReleaseAfterAuthorizationAndRollsBackReservation(t *testing.T) {
	f, destination, _ := readyAMLInventory(t)
	request := f.allocationRequest(t, destination, "50")
	rollback := errors.New("synthetic outer failure")
	err := postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		_, err := f.repo.ReserveAMLInternalAllocation(ctx, request)
		require.NoError(t, err)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	plan, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err, "outer rollback must also restore the inventory revision")
	auth, unsigned := f.allocationAuthorization(t, request)
	_, err = newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.NoError(t, err)
	err = f.repo.CancelAMLInternalAllocation(f.ctx, plan.ID, "signature outcome not known", "operator")
	require.ErrorContains(t, err, "after authorization began")
	var cancellations int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_allocation_cancellations`).Scan(&cancellations))
	require.Zero(t, cancellations, "failed cancellation audit and status must roll back together")
	require.Equal(t, "90", f.riskBalance(t).Items[0].TechnicalHold.String())
}

func TestAMLInventoryLateHighAndUnpostedDestinationReceiptBlockAuthorization(t *testing.T) {
	f, destination, effectID := readyAMLInventory(t)
	request := f.allocationRequest(t, destination, "50")
	plan, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	unexpected := f.proof("TPartitionCo11ectorAddressbbbbbbbb", "unposted-unmatched-receipt", 1)
	f.save(t, unexpected)
	auth, unsigned := f.allocationAuthorization(t, request)
	_, err = newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.ErrorContains(t, err, "cancelled or stale", "zero physical ledger does not prove an empty chain destination")
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "source risk changed", "operator")
	require.NoError(t, err)
	// Complete the source job directly: another unmatched destination has a
	// separate pending check, whose queue order is intentionally independent.
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_jobs SET status='needs_attention' WHERE subject_id IN (SELECT subject_id FROM reg_crypto_aml_effect_checks c JOIN reg_crypto_aml_effects e ON e.id=c.effect_id WHERE e.wallet_id=$1)`, destination)
	require.NoError(t, err)
	f.complete(t, effectID, 9000, true)
	f.rebuild(t)
	_, err = newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.ErrorContains(t, err, "cancelled or stale")
	require.NoError(t, f.repo.CancelAMLInternalAllocation(f.ctx, plan.ID, "unsigned adverse plan cancelled", "operator"))
	require.True(t, f.riskBalance(t).Items[0].HasHighExposure)
	require.ErrorIs(t, RequireAMLTransferPlan(f.ctx, crypto.TransferIntent{SourceWalletID: request.SourceWalletID, Network: crypto.NetworkRef{ID: request.NetworkID}, Asset: crypto.AssetRef{TokenID: request.TokenID, ContractAddress: "test-contract"}}), ErrAMLRiskContractRequired)
}

func TestAMLInventoryRejectsForeignCustodyAndDifferentDestinationClass(t *testing.T) {
	f, destination, _ := readyAMLInventory(t)
	merchantB := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Isolated merchant')`, merchantB, merchantB.String())
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version)
 VALUES($1,$2,$3,TRUE,'routing-v1',$4,'test-mapping-v1')`, merchantB, f.input.Request.NetworkID, f.input.Request.TokenID, f.input.ProviderAccountID)
	require.NoError(t, err)
	foreign := f.collector(t, merchantB, "foreign-partition-collector")
	f.baseline(t, foreign)
	f.rebuild(t)
	request := f.allocationRequest(t, foreign, "50")
	_, err = f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.Error(t, err, "an empty wallet of another merchant cannot receive this allocation")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_sweep_attempts SET status='failed' WHERE id=$1`, request.SweepAttemptID)
	require.NoError(t, err)
	proof := f.proof("TPartitionCo11ectorAddressbbbbbbbb", "medium-destination", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, destination)
	f.creditNet(t, proof.Event.TxHash)
	f.complete(t, effectID, 5000, true)
	f.rebuild(t)
	require.Equal(t, aml.Medium, f.class(t, destination))
	request = f.allocationRequest(t, destination, "50")
	_, err = f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.ErrorContains(t, err, "destination risk mismatch", "automatic consolidation never merges low principal into medium")
	var plans int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_internal_allocations`).Scan(&plans))
	require.Zero(t, plans)
}

func TestAMLInventorySQLReservationPreservesNumeric78IntegerQuotient(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	net := types.MustCryptoAmount("999999999999999999999999999999999999999999999999999999999999999999999999999998")
	proof := f.proof(f.input.Request.Address, "numeric78-deposit", 1)
	proof.Event.Amount = net.Add(types.MustCryptoAmount("1"))
	proofID, effectID := f.save(t, proof)
	f.postPaymentFee(t, proof, proofID, f.input.WalletID, "1")
	f.attribute(t, proof.Event.TxHash, net.String(), "1")
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	destination := f.collector(t, f.input.Request.MerchantID, "TPartitionCo11ectorAddressbbbbbbbb")
	f.baseline(t, destination)
	f.rebuild(t)
	request := f.allocationRequest(t, destination, net.String())
	plan, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, net.Sub(types.MustCryptoAmount("1")).String(), plan.Partition.Principal.String())
	require.Equal(t, "1", plan.Partition.ServiceFee.String())
}

func TestAMLInventoryFailedApplyKeepsReservationAndDoesNotPoisonTheQueue(t *testing.T) {
	f, destination, _ := readyAMLInventory(t)
	request := f.allocationRequest(t, destination, "50")
	plan, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	f.proveAllocatedSweep(t, request, true)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_inventory_positions SET principal=0 WHERE wallet_id=$1`, request.SourceWalletID)
	require.NoError(t, err)
	progressed, err := f.repo.CaptureNextAMLInventoryAllocation(f.ctx)
	require.True(t, progressed)
	require.Error(t, err)
	var status, destinationNet string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_aml_internal_allocations WHERE id=$1`, plan.ID).Scan(&status))
	require.Equal(t, "reserved", status)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT principal::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1`, destination).Scan(&destinationNet))
	require.Equal(t, "0", destinationNet, "a failed component debit cannot leave a partial destination credit")
	var attempts int
	var deferred bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT attempts,next_attempt_at>NOW() FROM reg_crypto_aml_inventory_queue WHERE basis_id=$1`, plan.ID).Scan(&attempts, &deferred))
	require.Equal(t, 1, attempts)
	require.True(t, deferred)
	other := f.collector(t, f.input.Request.MerchantID, "unrelated-inventory")
	f.baseline(t, other)
	proof := f.proof("unrelated-inventory", strings.Repeat("cd", 32), 1)
	proofID, _ := f.save(t, proof)
	f.postPayment(t, proof, proofID, other)
	f.creditNet(t, proof.Event.TxHash)
	progressed, err = f.repo.CaptureNextAMLInventoryAllocation(f.ctx)
	require.NoError(t, err, "unrelated payment inventory must progress while the broken plan is deferred")
	require.True(t, progressed)
	var otherPrincipal string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT principal::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1`, other).Scan(&otherPrincipal))
	require.Equal(t, "90", otherPrincipal)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_inventory_positions SET principal=90 WHERE wallet_id=$1`, request.SourceWalletID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_inventory_queue SET next_attempt_at=NOW() WHERE basis_id=$1`, plan.ID)
	require.NoError(t, err)
	f.rebuild(t)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_aml_internal_allocations WHERE id=$1`, plan.ID).Scan(&status))
	require.Equal(t, "applied", status)
	var financialReceipts int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements`).Scan(&financialReceipts))
	require.Equal(t, 2, financialReceipts, "allocation recovery cannot repeat either customer's net credit")
}

func TestAMLInventoryFinalFeeWakeupCannotBeLostDuringCapture(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "late-fee-wakeup", 1)
	proofID, _ := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	feeDone := make(chan error, 1)
	err := postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		_, err := q.Exec(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND token_id=$2 FOR UPDATE`, f.input.Request.MerchantID, f.input.Request.TokenID)
		if err != nil {
			return err
		}
		var ready int
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM crypto_aml_allocatable_payments`).Scan(&ready))
		require.Zero(t, ready, "the worker's first evidence snapshot is missing the payment attribution")
		go func() {
			_, err := f.pool.Exec(f.ctx, `/*allocation-wakeup*/ `+amlAttributionSQL, proof.Event.TxHash, "90", "10")
			feeDone <- err
		}()
		require.Eventually(t, func() bool {
			var waiting bool
			err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '/*allocation-wakeup*/%')`).Scan(&waiting)
			return err == nil && waiting
		}, 3*time.Second, 10*time.Millisecond, "financial wakeup must serialize with the worker's scope lock")
		// The worker can now acknowledge its old, incomplete queue entry. The
		// concurrent final financial movement must requeue after this commit.
		_, err = q.Exec(ctx, `DELETE FROM reg_crypto_aml_inventory_queue WHERE kind='payment'`)
		return err
	})
	require.NoError(t, err)
	require.NoError(t, <-feeDone)
	f.rebuild(t)
	var principal string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT principal::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1`, f.input.WalletID).Scan(&principal))
	require.Equal(t, "90", principal)
}

// saveTRONAccountingProof stores a fixture transfer as decoder-v2 TRON evidence
// with a bound accounting identity. The paused reader registration exists only
// while the proof is saved, so later fixture payments keep the default schema.
func (f amlRoutingFixture) saveTRONAccountingProof(t *testing.T, proof crypto.TransferVerification) id.ID {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,1,'checkpoint',1,'paused',2)`, proof.Event.NetworkID)
	require.NoError(t, err)
	proof.DecoderVersion, proof.Event.DecoderVersion = 2, 2
	for i := range proof.Sources {
		proof.Sources[i].DecoderVersion = 2
		proof.Sources[i].SourceDigest = "signed-sweep"
	}
	require.NoError(t, proof.BindTRONAccountingIdentity())
	proofID, _ := f.save(t, proof)
	_, err = f.pool.Exec(f.ctx, `ALTER TABLE sys_crypto_block_ingestion DISABLE TRIGGER USER;
		DELETE FROM sys_crypto_block_ingestion WHERE network_id='`+proof.Event.NetworkID.String()+`';
		ALTER TABLE sys_crypto_block_ingestion ENABLE TRIGGER USER`)
	require.NoError(t, err)
	return proofID
}
