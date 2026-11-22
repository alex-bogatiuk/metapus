//go:build integration

package crypto_repo

import (
	"context"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestAMLInventoryCaptureSkipsProducerScopeWithoutLockingQueue(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "scope-busy-payment", 1)
	proofID, _ := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	holder, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(context.Background()) }()
	_, err = holder.Exec(f.ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 FOR UPDATE`, f.input.Request.MerchantID, f.input.Request.NetworkID, f.input.Request.TokenID)
	require.NoError(t, err)
	bounded, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	progressed, err := f.repo.CaptureNextAMLInventoryAllocation(bounded)
	require.NoError(t, err)
	require.False(t, progressed)
	_, err = holder.Exec(f.ctx, `SELECT basis_id FROM reg_crypto_aml_inventory_queue WHERE kind='payment' FOR UPDATE NOWAIT`)
	require.NoError(t, err, "capture must not keep a queue row while skipping its busy producer scope")
	require.NoError(t, holder.Rollback(f.ctx))
	f.creditNet(t, proof.Event.TxHash)
	progressed, err = f.repo.CaptureNextAMLInventoryAllocation(f.ctx)
	require.NoError(t, err)
	require.True(t, progressed)
	var principal string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT principal::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1 AND token_id=$2", f.input.WalletID, f.input.Request.TokenID).Scan(&principal))
	require.Equal(t, "90", principal)
}

func TestAMLResourceFinalWakeupSurvivesRealIncompleteCapture(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	hash := signResourceFunding(t, f, target, fee, funding)
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT address FROM cat_wallets WHERE id=$1", target).Scan(&address))
	proof := nativeResourceProof(t, f, address, hash, "20")
	proof.Event.FromAddress, proof.Event.Direction = f.input.Request.Address, crypto.TransferDirectionInternal
	f.save(t, proof)
	tm := postgres.MustGetTxManager(f.ctx)
	assertAccountingLockOrder(t, f.ctx, f.pool,
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, "SELECT fn_crypto_lock_custody_scopes(ARRAY[$1::uuid])", f.input.WalletID)
			return err
		}, func(ctx context.Context) error {
			var ready int
			require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT count(*) FROM crypto_aml_ready_resource_budgets WHERE id=$1", budget.ID).Scan(&ready))
			require.Zero(t, ready)
			progressed, err := f.repo.CaptureNextAMLInventoryAllocation(ctx)
			require.True(t, progressed)
			if err != nil {
				return err
			}
			var pending int
			require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT count(*) FROM reg_crypto_aml_inventory_queue WHERE kind='resource' AND basis_id=$1", budget.ID).Scan(&pending))
			require.Zero(t, pending, "the incomplete entry is acknowledged before the final producer commits")
			return nil
		}, func(ctx context.Context) error {
			local := f
			local.ctx = ctx
			return recordResourceEvidence(local, fee, funding, hash, "succeeded", "3", true)
		})
	progressed, err := f.repo.CaptureNextAMLInventoryAllocation(f.ctx)
	require.NoError(t, err)
	require.True(t, progressed, "the producer must requeue after capture acknowledgement")
	var status string
	var facts int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status,(SELECT count(*) FROM reg_crypto_aml_resource_facts WHERE budget_id=$1) FROM reg_crypto_aml_resource_budgets WHERE id=$1`, budget.ID).Scan(&status, &facts))
	require.Equal(t, "settled", status)
	require.Equal(t, 2, facts)
	require.Equal(t, []string{"77", "20"}, resourceAmounts(t, f, target))
}

func TestAMLResourceCaptureRejectsStaleOuterSnapshotBeforeAcknowledgement(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	hash := signResourceFunding(t, f, target, fee, funding)
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT address FROM cat_wallets WHERE id=$1", target).Scan(&address))
	proof := nativeResourceProof(t, f, address, hash, "20")
	proof.Event.FromAddress, proof.Event.Direction = f.input.Request.Address, crypto.TransferDirectionInternal
	f.save(t, proof)
	tm := postgres.MustGetTxManager(f.ctx)
	opts := postgres.DefaultTxOptions()
	opts.IsolationLevel = pgx.RepeatableRead
	err = tm.RunInTransactionWithOptions(f.ctx, opts, func(ctx context.Context) error {
		var ready int
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT count(*) FROM crypto_aml_ready_resource_budgets WHERE id=$1", budget.ID).Scan(&ready))
		require.Zero(t, ready)
		// This producer uses the original context and commits on another
		// connection after the consumer's repeatable-read snapshot is fixed.
		require.NoError(t, recordResourceEvidence(f, fee, funding, hash, "succeeded", "3", true))
		progressed, err := f.repo.CaptureNextAMLInventoryAllocation(ctx)
		require.False(t, progressed)
		return err
	})
	require.ErrorContains(t, err, "read committed")
	var pending int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_aml_inventory_queue WHERE kind='resource' AND basis_id=$1", budget.ID).Scan(&pending))
	require.Equal(t, 1, pending, "stale snapshot must not acknowledge the producer's last wakeup")
	progressed, err := f.repo.CaptureNextAMLInventoryAllocation(f.ctx)
	require.NoError(t, err)
	require.True(t, progressed)
	require.Equal(t, []string{"77", "20"}, resourceAmounts(t, f, target))
}
