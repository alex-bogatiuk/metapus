//go:build integration

package crypto_repo_test

import (
	"context"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/outbound_repo"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func activationSnapshot(t *testing.T, f crypto_repo.ActivationSelectorFixture, key id.ID) string {
	t.Helper()
	var value string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT to_jsonb(a)::text FROM reg_crypto_account_activations a WHERE id=$1`, key).Scan(&value))
	return value
}

func TestActivationSelectorSkipsBusyCompleteFootprintWithoutPartialLocks(t *testing.T) {
	for _, resource := range []string{"wallet", "activation", "advisory", "policy", "payer", "scope"} {
		t.Run(resource, func(t *testing.T) {
			f := crypto_repo.NewActivationSelectorFixture(t, resource == "scope")
			a, b := f.SeedTasks(t)
			tm := postgres.MustGetTxManager(f.Ctx)
			ready, done := make(chan error, 1), make(chan error, 1)
			release := make(chan struct{})
			go func() {
				done <- tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
					q := tm.GetQuerier(ctx)
					var err error
					switch resource {
					case "wallet":
						_, err = q.Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.A)
					case "activation":
						_, err = q.Exec(ctx, `SELECT id FROM reg_crypto_account_activations WHERE id=$1 FOR UPDATE`, a)
					case "advisory":
						err = postgres.NewDocLocker().LockDocument(ctx, "CryptoAccountActivationTask", a)
					case "policy":
						_, err = q.Exec(ctx, `SELECT network_id FROM sys_crypto_wallet_pool_policies WHERE network_id=$1 FOR UPDATE`, f.Network)
					case "payer":
						_, err = q.Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.Hot)
					case "scope":
						_, err = q.Exec(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 FOR UPDATE`, f.Merchant, f.Network)
					}
					ready <- err
					if err != nil {
						return err
					}
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			defer func() { close(release); require.NoError(t, <-done) }()
			require.NoError(t, <-ready)
			bounded, cancel := context.WithTimeout(f.Ctx, 3*time.Second)
			defer cancel()
			require.NoError(t, tm.RunInTransaction(bounded, func(ctx context.Context) error {
				tasks, err := outbound_repo.NewOutboundRepo().ClaimAccountActivationTasks(ctx, "selector", 1)
				require.NoError(t, err)
				if resource == "policy" || resource == "payer" || resource == "scope" {
					require.Empty(t, tasks)
				} else {
					require.Len(t, tasks, 1)
					require.Equal(t, b, tasks[0].ActivationID)
				}
				// A separate transaction must be able to use resources of the
				// skipped candidate while the successful outer claim stays open.
				return tm.RunInTransaction(f.Ctx, func(other context.Context) error {
					if resource != "advisory" {
						acquired, err := postgres.NewDocLocker().TryLockDocument(other, "CryptoAccountActivationTask", a)
						require.NoError(t, err)
						require.True(t, acquired)
					}
					if resource != "wallet" {
						_, err := tm.GetQuerier(other).Exec(other, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE NOWAIT`, f.A)
						require.NoError(t, err)
					}
					return nil
				})
			}))
			var attempts int
			require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT activation_attempts FROM reg_crypto_account_activations WHERE id=$1`, a).Scan(&attempts))
			require.Zero(t, attempts)
		})
	}
}

func TestActivationSelectorRequiresCounterpartsAndKeepsReadyCapacity(t *testing.T) {
	f := crypto_repo.NewActivationSelectorFixture(t)
	repo := outbound_repo.NewOutboundRepo()
	_, err := f.Pool.Exec(f.Ctx, `UPDATE cat_tokens SET is_active=FALSE WHERE id=$1`, f.Native)
	require.NoError(t, err)
	tasks, err := repo.ClaimAccountActivationTasks(f.Ctx, "without-native", 10)
	require.NoError(t, err)
	require.Empty(t, tasks)
	var count int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_account_activations`).Scan(&count))
	require.Zero(t, count)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE cat_tokens SET is_active=TRUE WHERE id=$1; UPDATE cat_wallets SET is_active=FALSE WHERE id=$2`, pgx.QueryExecModeSimpleProtocol, f.Native, f.Hot)
	require.NoError(t, err)
	tasks, err = repo.ClaimAccountActivationTasks(f.Ctx, "without-payer", 10)
	require.NoError(t, err)
	require.Empty(t, tasks)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE cat_wallets SET is_active=TRUE WHERE id=$1; UPDATE sys_crypto_wallet_pool_policies SET target_ready=1 WHERE network_id=$2`, pgx.QueryExecModeSimpleProtocol, f.Hot, f.Network)
	require.NoError(t, err)
	tasks, err = repo.ClaimAccountActivationTasks(f.Ctx, "ready", 10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_account_activations`).Scan(&count))
	require.Equal(t, 1, count)
	again, err := repo.ClaimAccountActivationTasks(f.Ctx, "again", 10)
	require.NoError(t, err)
	require.Empty(t, again)
	before := activationSnapshot(t, f, tasks[0].ActivationID)
	require.NoError(t, repo.ApplyAccountActivationClaim(f.Ctx, tasks[0], outboundapp.ActivationClaimUpdate{Action: outboundapp.ActivationClaimDeferred, Delay: time.Second}))
	deferred := activationSnapshot(t, f, tasks[0].ActivationID)
	require.NotEqual(t, before, deferred)
	require.NoError(t, repo.DeferAccountActivation(f.Ctx, tasks[0].ActivationID, time.Second))
	require.Equal(t, deferred, activationSnapshot(t, f, tasks[0].ActivationID))
}

func TestActivationStateWalletAndPolicyPrecedeTaskRow(t *testing.T) {
	for _, firstResource := range []string{"wallet", "policy"} {
		t.Run(firstResource, func(t *testing.T) {
			f := crypto_repo.NewActivationSelectorFixture(t)
			a, _ := f.SeedTasks(t)
			tm := postgres.MustGetTxManager(f.Ctx)
			f.AssertLockOrder(t, func(ctx context.Context) error {
				if firstResource == "wallet" {
					return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, f.A)
				}
				_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT network_id FROM sys_crypto_wallet_pool_policies WHERE network_id=$1 FOR UPDATE`, f.Network)
				return err
			}, func(ctx context.Context) error {
				query := `SELECT id FROM reg_crypto_account_activations WHERE id=$1 FOR UPDATE`
				key := a
				if firstResource == "policy" {
					query = `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`
					key = f.A
				}
				_, err := tm.GetQuerier(ctx).Exec(ctx, query, key)
				return err
			}, func(ctx context.Context) error {
				return outbound_repo.NewOutboundRepo().MarkAccountActivationHeld(ctx, a, "synthetic hold")
			})
		})
	}
}

func TestActivationTerminalAndFailedReplayPreserveFirstState(t *testing.T) {
	f := crypto_repo.NewActivationSelectorFixture(t)
	a, b := f.SeedTasks(t)
	repo := outbound_repo.NewOutboundRepo()
	require.NoError(t, repo.MarkAccountActivationExternallyActive(f.Ctx, a, f.Native))
	before := activationSnapshot(t, f, a)
	require.NoError(t, repo.MarkAccountActivationExternallyActive(f.Ctx, a, f.Native))
	require.NoError(t, repo.MarkAccountActivationHeld(f.Ctx, a, "stale hold"))
	require.NoError(t, repo.MarkAccountActivationFailed(f.Ctx, a, "stale failure"))
	require.Equal(t, before, activationSnapshot(t, f, a))
	require.NoError(t, repo.MarkAccountActivationFailed(f.Ctx, b, "original failure"))
	failed := activationSnapshot(t, f, b)
	require.NoError(t, repo.MarkAccountActivationFailed(f.Ctx, b, "original failure"))
	require.NoError(t, repo.MarkAccountActivationHeld(f.Ctx, b, "stale hold"))
	require.Equal(t, failed, activationSnapshot(t, f, b))
	require.ErrorContains(t, repo.MarkAccountActivationFailed(f.Ctx, b, "another failure"), "original reason")
}

func TestActivationTerminalRejectsSupersededSuccess(t *testing.T) {
	f := crypto_repo.NewActivationSelectorFixture(t)
	a, _ := f.SeedTasks(t)
	repo := outbound_repo.NewOutboundRepo()
	require.NoError(t, repo.MarkAccountActivationFailed(f.Ctx, a, "original failure"))
	next := id.New()
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_account_activations(id,wallet_id,network_id,status) VALUES($1,$2,$3,'claimed'); UPDATE cat_wallets SET chain_activation_status='activating' WHERE id=$2`, pgx.QueryExecModeSimpleProtocol, next, f.A, f.Network)
	require.NoError(t, err)
	oldState, nextState := activationSnapshot(t, f, a), activationSnapshot(t, f, next)
	require.ErrorContains(t, repo.MarkAccountActivationActive(f.Ctx, a), "superseded")
	require.ErrorContains(t, repo.MarkAccountActivationExternallyActive(f.Ctx, a, f.Native), "superseded")
	require.Equal(t, oldState, activationSnapshot(t, f, a))
	require.Equal(t, nextState, activationSnapshot(t, f, next))
	require.NoError(t, repo.MarkAccountActivationExternallyActive(f.Ctx, next, f.Native))
}

func TestActivationLinkedOperationIsExactAndCannotBeRebound(t *testing.T) {
	f := crypto_repo.NewActivationSelectorFixture(t)
	a, b := f.SeedTasks(t)
	wrong, valid := id.New(), id.New()
	for key, basis := range map[id.ID]id.ID{wrong: b, valid: a} {
		_, err := f.Pool.Exec(f.Ctx, `INSERT INTO doc_crypto_network_operations(id,basis_type,basis_id,operation_type,network_id,target_wallet_id,target_address,source_wallet_id,cost_token_id,cost_amount,tx_hash)
 SELECT $1,'CryptoAccountActivationTask',$2,'wallet_activation',$3,w.id,w.address,$4,$5,0,$1::uuid::text FROM cat_wallets w WHERE w.id=$6`, key, basis, f.Network, f.Hot, f.Native, f.A)
		require.NoError(t, err)
	}
	_, err := f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET network_operation_id=$2 WHERE id=$1`, a, wrong)
	require.ErrorContains(t, err, "does not match")
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET network_operation_id=$2 WHERE id=$1`, a, valid)
	require.NoError(t, err)
	before := activationSnapshot(t, f, a)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET network_operation_id=NULL WHERE id=$1`, a)
	require.ErrorContains(t, err, "identity is immutable")
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_account_activations SET network_operation_id=$2 WHERE id=$1`, a, wrong)
	require.ErrorContains(t, err, "identity is immutable")
	require.Equal(t, before, activationSnapshot(t, f, a))
}

func TestActivationSelectionRejectsStaleOuterSnapshot(t *testing.T) {
	f := crypto_repo.NewActivationSelectorFixture(t)
	tm := postgres.MustGetTxManager(f.Ctx)
	opts := postgres.DefaultTxOptions()
	opts.IsolationLevel = pgx.RepeatableRead
	err := tm.RunInTransactionWithOptions(f.Ctx, opts, func(ctx context.Context) error {
		_, err := outbound_repo.NewOutboundRepo().ClaimAccountActivationTasks(ctx, "stale", 2)
		return err
	})
	require.ErrorContains(t, err, "read committed")
	var count int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_account_activations`).Scan(&count))
	require.Zero(t, count)
}
