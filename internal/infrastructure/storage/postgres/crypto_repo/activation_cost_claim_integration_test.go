//go:build integration

package crypto_repo_test

import (
	"context"
	"database/sql"
	"io/fs"
	"sync"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"
)

func activationCostClaimFixture(t *testing.T) (crypto_repo.ActivationSelectorFixture, id.ID) {
	t.Helper()
	f := crypto_repo.NewActivationSelectorFixture(t)
	key, _ := f.SeedTasks(t)
	operation := id.New()
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO doc_crypto_network_operations(id,basis_type,basis_id,operation_type,network_id,target_wallet_id,target_address,source_wallet_id,cost_token_id,cost_amount,tx_hash)
 SELECT $1,'CryptoAccountActivationTask',$2,'wallet_activation',$3,w.id,w.address,$4,$5,0,$1::uuid::text FROM cat_wallets w WHERE w.id=$6`, operation, key, f.Network, f.Hot, f.Native, f.A)
	require.NoError(t, err)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET network_operation_id=$2,status='confirmed',cost_status='unknown' WHERE id=$1`, key, operation)
	require.NoError(t, err)
	return f, key
}
func claimActivationCost(t *testing.T, f crypto_repo.ActivationSelectorFixture) outboundapp.ActivationCostRepairTask {
	t.Helper()
	tasks, err := outbound_repo.NewOutboundRepo().ClaimActivationCostRepairs(f.Ctx, "same-worker", 1)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.False(t, id.IsNil(tasks[0].CostClaimToken))
	return tasks[0]
}
func rejectActivationCost(t *testing.T, f crypto_repo.ActivationSelectorFixture, task outboundapp.ActivationCostRepairTask) {
	t.Helper()
	r := outbound_repo.NewOutboundRepo()
	before := activationSnapshot(t, f, task.ActivationID)
	require.ErrorContains(t, r.PrepareActivationCostRepair(f.Ctx, task), "claim is missing or stale")
	require.ErrorContains(t, r.MarkActivationCostRepairHeld(f.Ctx, task, "late receipt"), "claim is missing or stale")
	require.Equal(t, before, activationSnapshot(t, f, task.ActivationID))
}

func TestActivationCostClaimFencesExpiredAndChangedAttempt(t *testing.T) {
	f, key := activationCostClaimFixture(t)
	r := outbound_repo.NewOutboundRepo()
	first := claimActivationCost(t, f)
	next, err := r.ClaimActivationCostRepairs(f.Ctx, "another-worker", 2)
	require.NoError(t, err)
	require.Empty(t, next)
	for _, change := range []func(*outboundapp.ActivationCostRepairTask){func(t *outboundapp.ActivationCostRepairTask) { t.CostClaimToken = id.New() }, func(t *outboundapp.ActivationCostRepairTask) { t.CostClaimToken = id.Nil() }, func(t *outboundapp.ActivationCostRepairTask) { t.TxHash = "another-hash" }, func(t *outboundapp.ActivationCostRepairTask) { t.NetworkID = id.New() }} {
		altered := first
		change(&altered)
		rejectActivationCost(t, f, altered)
	}
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET cost_lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, key)
	require.NoError(t, err)
	rejectActivationCost(t, f, first)
	second := claimActivationCost(t, f)
	require.NotEqual(t, first.CostClaimToken, second.CostClaimToken)
	rejectActivationCost(t, f, first)
	require.NoError(t, r.PrepareActivationCostRepair(f.Ctx, second))
	require.NoError(t, r.MarkActivationCostRepairHeld(f.Ctx, second, "receipt pending"))
	rejectActivationCost(t, f, second)
	next, err = r.ClaimActivationCostRepairs(f.Ctx, "early-retry", 1)
	require.NoError(t, err)
	require.Empty(t, next)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET cost_checked_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, key)
	require.NoError(t, err)
	third := claimActivationCost(t, f)
	require.NotEqual(t, second.CostClaimToken, third.CostClaimToken)
}

func TestActivationCostClaimConcurrentAcquisition(t *testing.T) {
	f, _ := activationCostClaimFixture(t)
	r := outbound_repo.NewOutboundRepo()
	var wg sync.WaitGroup
	tasks := make(chan []outboundapp.ActivationCostRepairTask, 4)
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() { rows, err := r.ClaimActivationCostRepairs(f.Ctx, "worker", 1); tasks <- rows; errs <- err })
	}
	wg.Wait()
	close(tasks)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	count := 0
	for rows := range tasks {
		count += len(rows)
	}
	require.Equal(t, 1, count)
}

func TestActivationCostClaimWallClockAndIsolation(t *testing.T) {
	f, key := activationCostClaimFixture(t)
	task := claimActivationCost(t, f)
	r := outbound_repo.NewOutboundRepo()
	tm := postgres.MustGetTxManager(f.Ctx)
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		var started string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT now()::text`).Scan(&started))
		_, err := f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET cost_lease_expires_at=clock_timestamp()-interval '1 microsecond' WHERE id=$1`, key)
		require.NoError(t, err)
		var oldClockWouldAccept bool
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT cost_lease_expires_at>now() AND cost_lease_expires_at<clock_timestamp() FROM reg_crypto_account_activations WHERE id=$1`, key).Scan(&oldClockWouldAccept))
		require.True(t, oldClockWouldAccept)
		require.ErrorContains(t, r.PrepareActivationCostRepair(ctx, task), "claim is missing or stale")
		return nil
	}))
	current := claimActivationCost(t, f)
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		require.NoError(t, r.MarkActivationCostRepairHeld(ctx, current, "pending receipt"))
		var checkedAfterBegin bool
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT cost_checked_at>now() FROM reg_crypto_account_activations WHERE id=$1`, key).Scan(&checkedAfterBegin))
		require.True(t, checkedAfterBegin, "held retry delay must start at the actual state update")
		return nil
	}))
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
		require.NoError(t, err)
		_, err = r.ClaimActivationCostRepairs(ctx, "stale", 1)
		require.ErrorContains(t, err, "read committed")
		require.ErrorContains(t, r.PrepareActivationCostRepair(ctx, task), "read committed")
		return nil
	}))
}

func TestActivationCostClaimResourcesPrecedeTask(t *testing.T) {
	for _, resource := range []string{"policy", "payer"} {
		t.Run(resource, func(t *testing.T) {
			f, key := activationCostClaimFixture(t)
			task := claimActivationCost(t, f)
			tm := postgres.MustGetTxManager(f.Ctx)
			f.AssertLockOrder(t, func(ctx context.Context) error {
				query, arg := `SELECT network_id FROM sys_crypto_wallet_pool_policies WHERE network_id=$1 FOR UPDATE`, f.Network
				if resource == "payer" {
					query, arg = `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.Hot
				}
				_, err := tm.GetQuerier(ctx).Exec(ctx, query, arg)
				return err
			}, func(ctx context.Context) error {
				_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM reg_crypto_account_activations WHERE id=$1 FOR UPDATE`, key)
				return err
			}, func(ctx context.Context) error {
				return outbound_repo.NewOutboundRepo().MarkActivationCostRepairHeld(ctx, task, "pending")
			})
		})
	}
}

func TestActivationCostClaimMigrationPreservesLegacyLease(t *testing.T) {
	f, key := activationCostClaimFixture(t)
	r := outbound_repo.NewOutboundRepo()
	db, err := sql.Open("pgx", f.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(f.Ctx, 94)
	require.NoError(t, err)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET cost_status='repairing',cost_claimed_by='legacy',cost_claimed_at=now(),cost_lease_expires_at=now()+interval '5 minutes' WHERE id=$1`, key)
	require.NoError(t, err)
	before := activationSnapshot(t, f, key)
	_, err = provider.Up(f.Ctx)
	require.NoError(t, err)
	var after string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT (to_jsonb(a)-'cost_claim_token')::text FROM reg_crypto_account_activations a WHERE id=$1`, key).Scan(&after))
	require.Equal(t, before, after)
	tasks, err := r.ClaimActivationCostRepairs(f.Ctx, "new-worker", 1)
	require.NoError(t, err)
	require.Empty(t, tasks)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET cost_claim_token=$2,cost_status='recorded' WHERE id=$1`, key, id.New())
	require.Error(t, err)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET cost_lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, key)
	require.NoError(t, err)
	claimActivationCost(t, f)
}
