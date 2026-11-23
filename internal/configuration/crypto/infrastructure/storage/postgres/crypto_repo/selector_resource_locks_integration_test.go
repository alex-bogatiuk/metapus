//go:build integration

package crypto_repo

import (
	"context"
	"sort"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"

	"github.com/stretchr/testify/require"
)

func TestSelectorResourceLockFailureReleasesWholePrefix(t *testing.T) {
	f := newAMLRoutingFixture(t, false, crypto.ChainFamilyEVM)
	second := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,custody_merchant_id)
		SELECT $1,$1::uuid::text,'Selector peer',network_id,$1::uuid::text,'selector-key',custody_merchant_id FROM cat_wallets WHERE id=$2`, second, f.input.WalletID)
	require.NoError(t, err)
	wallets := []id.ID{f.input.WalletID, second}
	sort.Slice(wallets, func(i, j int) bool { return wallets[i].String() < wallets[j].String() })
	holder, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(context.Background()) }()
	_, err = holder.Exec(f.ctx, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE", wallets[1])
	require.NoError(t, err)
	selector, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = selector.Rollback(context.Background()) }()
	var acquired bool
	require.NoError(t, selector.QueryRow(f.ctx, "SELECT fn_crypto_try_lock_custody_wallets($1::uuid[])", wallets).Scan(&acquired))
	require.False(t, acquired)
	// Keep selector's outer transaction open: only the failed attempt should
	// roll back, releasing the scope and first wallet as well as partial locks.
	_, err = f.pool.Exec(f.ctx, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE NOWAIT", wallets[0])
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 FOR UPDATE NOWAIT`, f.input.Request.MerchantID, f.input.Request.NetworkID)
	require.NoError(t, err)
	require.NoError(t, holder.Rollback(f.ctx))
	require.NoError(t, selector.QueryRow(f.ctx, "SELECT fn_crypto_try_lock_custody_wallets($1::uuid[])", wallets).Scan(&acquired))
	require.True(t, acquired)
	_, err = f.pool.Exec(f.ctx, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE NOWAIT", wallets[0])
	require.ErrorContains(t, err, "55P03")
}

func TestSelectorResourceLocksRejectMissingTargetsAndInvalidFootprints(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	for _, keys := range [][]id.ID{nil, {}, {f.request.Basis.WalletID, id.New()}} {
		var acquired bool
		err := f.pool.QueryRow(f.ctx, "SELECT fn_crypto_try_lock_custody_wallets($1::uuid[])", keys).Scan(&acquired)
		require.ErrorContains(t, err, "23514")
	}
}

func TestSelectorResourceLocksMergeRequestScopeBeforeWallets(t *testing.T) {
	f := newAMLRoutingFixture(t, false, crypto.ChainFamilyEVM)
	merchant := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Request scope owner')`, merchant)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version)
		SELECT $1,network_id,token_id,FALSE,policy_version,provider_account_id,mapping_version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$2 AND network_id=$3 AND token_id=$4`, merchant, f.input.Request.MerchantID, f.input.Request.NetworkID, f.input.Request.TokenID)
	require.NoError(t, err)
	holder, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(context.Background()) }()
	_, err = holder.Exec(f.ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 FOR UPDATE`, merchant, f.input.Request.NetworkID)
	require.NoError(t, err)
	selector, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = selector.Rollback(context.Background()) }()
	var acquired bool
	require.NoError(t, selector.QueryRow(f.ctx, `SELECT fn_crypto_try_lock_custody_wallets($1::uuid[],$2,$3)`, []id.ID{f.input.WalletID}, merchant, f.input.Request.NetworkID).Scan(&acquired))
	require.False(t, acquired)
	_, err = f.pool.Exec(f.ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 FOR UPDATE NOWAIT`, f.input.Request.MerchantID, f.input.Request.NetworkID)
	require.NoError(t, err, "busy request scope must release the earlier wallet-owner scope")
	_, err = f.pool.Exec(f.ctx, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE NOWAIT", f.input.WalletID)
	require.NoError(t, err)
	require.NoError(t, holder.Rollback(f.ctx))
	_, err = selector.Exec(f.ctx, `SELECT fn_crypto_lock_withdrawal_wallets($1::uuid[],$2,$3)`, []id.ID{f.input.WalletID}, merchant, f.input.Request.NetworkID)
	require.NoError(t, err)
	for _, owner := range []id.ID{merchant, f.input.Request.MerchantID} {
		_, err = f.pool.Exec(f.ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 FOR UPDATE NOWAIT`, owner, f.input.Request.NetworkID)
		require.ErrorContains(t, err, "55P03")
	}
}
