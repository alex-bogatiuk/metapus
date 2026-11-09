//go:build integration

package crypto_repo

import (
	"sync"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"

	"github.com/stretchr/testify/require"
)

func batchFixture(t *testing.T, cap string, age int) (amlRoutingFixture, aml.Batch, id.ID) {
	t.Helper()
	f, collector, seed := readyAMLInventory(t)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_network_policies(network_id) VALUES($1) ON CONFLICT DO NOTHING`, f.input.Request.NetworkID)
	require.NoError(t, err)
	version, err := f.repo.ConfigureAMLBatchPolicy(f.ctx, aml.BatchPolicyRequest{MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID, TokenID: f.input.Request.TokenID, Class: aml.Low, MaxGross: types.MustCryptoAmount(cap), MaxAgeSeconds: age, Actor: "integration-test"})
	require.NoError(t, err)
	batch, err := f.repo.OpenAMLBatch(f.ctx, aml.OpenBatchRequest{ID: id.New(), MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID, TokenID: f.input.Request.TokenID, CollectorID: collector, Class: aml.Low, PolicyVersion: version, ScopeVersion: 1, Actor: "integration-test"})
	require.NoError(t, err)
	return f, batch, seed
}

func batchAllocation(t *testing.T, f amlRoutingFixture, b aml.Batch, amount string) aml.InternalAllocationRequest {
	t.Helper()
	r := f.allocationRequest(t, b.CollectorID, amount)
	r.BatchID = &b.ID
	r.BatchRevision = b.Revision
	r.BatchExecutionRevision = b.ExecutionRevision
	return r
}

func TestAMLBatchClosedIntakeRetainsUnknownAndAccountsRealizedTransfer(t *testing.T) {
	f, batch, seed := batchFixture(t, "60", 600)
	request := batchAllocation(t, f, batch, "60")
	plan, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	b, err := f.repo.ReadAMLBatch(f.ctx, f.input.Request.MerchantID, batch.ID)
	require.NoError(t, err)
	require.NotNil(t, b.IntakeClosedAt)
	require.False(t, b.ExecutionHeld)
	require.Equal(t, "60", b.Reserved.String())
	require.Equal(t, "0", b.AvailableCapacity().String())
	f.proveAllocatedSweep(t, request, true)
	require.Error(t, f.repo.CancelAMLInternalAllocation(f.ctx, plan.ID, "broadcast outcome unknown", "operator"))
	f.rebuild(t)
	b, err = f.repo.ReadAMLBatch(f.ctx, f.input.Request.MerchantID, batch.ID)
	require.NoError(t, err)
	require.Equal(t, "60", b.Committed.String())
	require.Equal(t, "0", b.Reserved.String())
	require.NotNil(t, b.IntakeClosedAt)
	_, err = f.repo.RequestAMLRescreen(f.ctx, seed, 1, "late adverse intelligence", "operator")
	require.NoError(t, err)
	f.complete(t, seed, 9000, true)
	f.rebuild(t)
	b, err = f.repo.ReadAMLBatch(f.ctx, f.input.Request.MerchantID, batch.ID)
	require.NoError(t, err)
	require.True(t, b.ExecutionHeld)
	require.Greater(t, b.ExecutionRevision, batch.ExecutionRevision)
	require.Equal(t, "60", b.Committed.String(), "risk update cannot rewrite realized lifetime intake")
}

func TestAMLBatchConcurrentCapacityCannotOversubscribe(t *testing.T) {
	f, batch, _ := batchFixture(t, "60", 600)
	other := f
	other.input.WalletID = id.New()
	other.input.Request.Address = "batch-second-source"
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,merchant_id,custody_merchant_id,address,vault_key_id) VALUES($1,$1::uuid::text,'Second source',$2,$3,$3,$4,'test-key')`, other.input.WalletID, f.input.Request.NetworkID, f.input.Request.MerchantID, other.input.Request.Address)
	require.NoError(t, err)
	other.baseline(t, other.input.WalletID)
	proof := other.proof(other.input.Request.Address, "batch-second-payment", 1)
	proofID, effect := other.save(t, proof)
	other.postPayment(t, proof, proofID, other.input.WalletID)
	other.creditNet(t, proof.Event.TxHash)
	other.complete(t, effect, 1000, true)
	other.rebuild(t)
	requests := []aml.InternalAllocationRequest{batchAllocation(t, f, batch, "40"), batchAllocation(t, other, batch, "40")}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() { _, errs[i] = f.repo.ReserveAMLInternalAllocation(f.ctx, requests[i]) })
	}
	wg.Wait()
	success := 0
	for _, err := range errs {
		if err == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
	b, err := f.repo.ReadAMLBatch(f.ctx, f.input.Request.MerchantID, batch.ID)
	require.NoError(t, err)
	require.Equal(t, "40", b.Reserved.String())
	require.Equal(t, "20", b.AvailableCapacity().String())
}

func TestAMLBatchMandatoryBindingCancellationAndPlannerRotation(t *testing.T) {
	f, batch, _ := batchFixture(t, "60", 600)
	unbound := f.allocationRequest(t, batch.CollectorID, "60")
	_, err := f.repo.ReserveAMLInternalAllocation(f.ctx, unbound)
	require.ErrorContains(t, err, "explicit batch")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_sweep_attempts SET status='expired' WHERE id=$1`, unbound.SweepAttemptID)
	require.NoError(t, err)
	request := batchAllocation(t, f, batch, "60")
	plan, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	require.NoError(t, f.repo.CancelAMLInternalAllocation(f.ctx, plan.ID, "unsigned plan cancelled", "operator"))
	b, err := f.repo.ReadAMLBatch(f.ctx, f.input.Request.MerchantID, batch.ID)
	require.NoError(t, err)
	require.Equal(t, "60", b.AvailableCapacity().String())
	require.NotNil(t, b.IntakeClosedAt, "cancel does not reopen an old collector")
	collector := f.collector(t, f.input.Request.MerchantID, "next-batch-collector")
	f.baseline(t, collector)
	f.rebuild(t)
	next, err := f.repo.OpenAMLBatch(f.ctx, aml.OpenBatchRequest{ID: id.New(), MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID, TokenID: f.input.Request.TokenID, CollectorID: collector, Class: aml.Low, PolicyVersion: 1, ScopeVersion: 1, Actor: "operator"})
	require.NoError(t, err)
	request = f.allocationRequest(t, collector, "40")
	// The planner creates the attempt, so remove only this synthetic unused helper row.
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_sweep_attempts WHERE id=$1`, request.SweepAttemptID)
	require.NoError(t, err)
	sweep := aml.BatchSweepRequest{SweepAttemptID: request.SweepAttemptID, MerchantID: request.MerchantID, NetworkID: request.NetworkID, TokenID: request.TokenID, SourceWalletID: request.SourceWalletID, Amount: request.Amount, SourceRevision: request.SourceRevision, RiskRevision: request.RiskRevision, ScopeVersion: request.ScopeVersion, Class: request.Class}
	planned, err := f.repo.ReserveAMLBatchSweep(f.ctx, sweep)
	require.NoError(t, err)
	again, err := f.repo.ReserveAMLBatchSweep(f.ctx, sweep)
	require.NoError(t, err)
	require.Equal(t, planned.ID, again.ID)
	var batchID id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT batch_id FROM reg_crypto_aml_internal_allocations WHERE id=$1`, planned.ID).Scan(&batchID))
	require.Equal(t, next.ID, batchID)
}

func TestAMLBatchUnsolicitedIncomingClosesAndExposesExcess(t *testing.T) {
	f, batch, _ := batchFixture(t, "50", 600)
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, batch.CollectorID).Scan(&address))
	proof := f.proof(address, "unsolicited-batch-transfer", 1)
	proof.Event.Amount = types.MustCryptoAmount("150")
	f.save(t, proof)
	b, err := f.repo.ReadAMLBatch(f.ctx, f.input.Request.MerchantID, batch.ID)
	require.NoError(t, err)
	require.True(t, b.ExecutionHeld)
	require.NotNil(t, b.IntakeClosedAt)
	require.Equal(t, "150", b.Unplanned.String())
	require.Equal(t, "0", b.AvailableCapacity().String())
	_, err = f.repo.ReserveAMLInternalAllocation(f.ctx, batchAllocation(t, f, batch, "10"))
	require.Error(t, err)
}

func TestAMLBatchAgeClosesIntakeAndPolicyChangeHoldsExecution(t *testing.T) {
	f, batch, _ := batchFixture(t, "100", 1)
	require.Eventually(t, func() bool {
		changed, err := f.repo.CloseNextExpiredAMLBatch(f.ctx)
		require.NoError(t, err)
		return changed
	}, 3*time.Second, 25*time.Millisecond)
	b, err := f.repo.ReadAMLBatch(f.ctx, f.input.Request.MerchantID, batch.ID)
	require.NoError(t, err)
	require.NotNil(t, b.IntakeClosedAt)
	require.False(t, b.ExecutionHeld)
	_, err = f.repo.ConfigureAMLBatchPolicy(f.ctx, aml.BatchPolicyRequest{MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID, TokenID: f.input.Request.TokenID, Class: aml.Low, ExpectedVersion: 1, MaxGross: types.MustCryptoAmount("200"), MaxAgeSeconds: 60, Actor: "operator"})
	require.NoError(t, err)
	b, err = f.repo.ReadAMLBatch(f.ctx, f.input.Request.MerchantID, batch.ID)
	require.NoError(t, err)
	require.True(t, b.ExecutionHeld)
}

func TestAMLBatchHeldCollectorPrincipalRemainsTechnicalHold(t *testing.T) {
	f, batch, _ := batchFixture(t, "100", 600)
	request := batchAllocation(t, f, batch, "60")
	_, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	f.proveAllocatedSweep(t, request, true)
	f.rebuild(t)
	require.Equal(t, "90", f.riskBalance(t).Items[0].Free.String())
	_, err = f.repo.ConfigureAMLBatchPolicy(f.ctx, aml.BatchPolicyRequest{MerchantID: f.input.Request.MerchantID, NetworkID: f.input.Request.NetworkID, TokenID: f.input.Request.TokenID, Class: aml.Low, ExpectedVersion: 1, MaxGross: types.MustCryptoAmount("200"), MaxAgeSeconds: 60, Actor: "operator"})
	require.NoError(t, err)
	b := f.riskBalance(t).Items[0]
	require.Equal(t, "90", b.Total.String())
	require.Equal(t, "54", b.TechnicalHold.String())
	require.Equal(t, "36", b.Free.String())
}

func TestAMLBatchAuthorizationRechecksCollectorReadiness(t *testing.T) {
	f, batch, _ := batchFixture(t, "100", 600)
	request := batchAllocation(t, f, batch, "40")
	_, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET is_active=FALSE WHERE id=$1`, batch.CollectorID)
	require.NoError(t, err)
	auth, unsigned := f.allocationAuthorization(t, request)
	_, err = newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.ErrorContains(t, err, "batch execution is held or stale")
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET is_active=TRUE WHERE id=$1`, batch.CollectorID)
	require.NoError(t, err)
	auth.Purpose = crypto.SigningPurposeWithdrawal
	_, err = newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.ErrorContains(t, err, "batch execution is held or stale")
}
