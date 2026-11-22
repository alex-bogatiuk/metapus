//go:build integration

package crypto_repo

import (
	"context"
	"testing"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"

	"github.com/stretchr/testify/require"
)

func TestFundingConfirmationBusyRowReleasesScopesAndWallets(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	_, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	signResourceFunding(t, f, target, fee, funding)
	holder, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(context.Background()) }()
	_, err = holder.Exec(f.ctx, "SELECT id FROM doc_crypto_native_fundings WHERE id=$1 FOR UPDATE", funding)
	require.NoError(t, err)
	selector, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = selector.Rollback(context.Background()) }()
	var acquired bool
	require.NoError(t, selector.QueryRow(f.ctx, "SELECT fn_crypto_try_lock_funding_confirmation($1)", funding).Scan(&acquired))
	require.False(t, acquired)
	for _, wallet := range []id.ID{f.input.WalletID, target} {
		_, err = f.pool.Exec(f.ctx, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE NOWAIT", wallet)
		require.NoError(t, err)
	}
	_, err = f.pool.Exec(f.ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 FOR UPDATE NOWAIT`, f.input.Request.MerchantID, f.input.Request.NetworkID)
	require.NoError(t, err, "the outer selector transaction must not retain its partial scope prefix")
	require.NoError(t, holder.Rollback(f.ctx))
	require.NoError(t, selector.QueryRow(f.ctx, "SELECT fn_crypto_try_lock_funding_confirmation($1)", funding).Scan(&acquired))
	require.True(t, acquired)
	_, err = f.pool.Exec(f.ctx, "SELECT id FROM doc_crypto_native_fundings WHERE id=$1 FOR UPDATE NOWAIT", funding)
	require.ErrorContains(t, err, "55P03")
}

func TestFundingExpectedTransferNudgeLocksScopeBeforeFunding(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	_, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	var networkIdentifier string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT network_identifier FROM cat_blockchain_networks WHERE id=$1", f.input.Request.NetworkID).Scan(&networkIdentifier))
	hash := signResourceFunding(t, f, target, fee, funding, networkIdentifier)
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT address FROM cat_wallets WHERE id=$1", target).Scan(&address))
	proof := nativeResourceProof(t, f, address, hash, "20")
	proof.Event.FromAddress = f.input.Request.Address
	tm := postgres.MustGetTxManager(f.ctx)
	require.NoError(t, tm.RunInTransaction(f.ctx, func(ctx context.Context) error {
		handled, err := NewExpectedTransferRepo().RouteExpectedTransfer(ctx, proof.Event)
		require.True(t, handled)
		return err
	}))
	assertAccountingLockOrder(t, f.ctx, f.pool,
		func(ctx context.Context) error { return NewCustodyRecognitionRepo().LockWallets(ctx, f.input.WalletID) },
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, "SELECT id FROM doc_crypto_native_fundings WHERE id=$1 FOR UPDATE", funding)
			return err
		}, func(ctx context.Context) error {
			count, err := NewExpectedTransferRepo().NudgePending(ctx, 50)
			require.Equal(t, 1, count)
			return err
		})
	var status string
	var movements int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status,(SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1) FROM doc_crypto_native_fundings WHERE id=$1`, funding).Scan(&status, &movements))
	require.Equal(t, "broadcast", status)
	require.Zero(t, movements)
}
