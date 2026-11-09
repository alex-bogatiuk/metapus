//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestAMLOwnedAddressRemainsObservedAfterCatalogDeactivation(t *testing.T) {
	ctx, pool := custodyTestDatabase(t)
	tm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, tm)
	rollback := errors.New("rollback watcher fixture")
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var network id.ID
		require.NoError(t, q.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks ORDER BY code LIMIT 1`).Scan(&network))
		merchant := id.New()
		_, err := q.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Watcher continuity')`, merchant)
		require.NoError(t, err)
		owned, legacy := "owned-"+id.New().String(), "legacy-"+id.New().String()
		for _, row := range []struct {
			address string
			owner   *id.ID
		}{{owned, &merchant}, {legacy, nil}} {
			wallet := id.New()
			_, err = q.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,custody_merchant_id,is_active) VALUES($1,$1::uuid::text,'Inactive watcher source',$2,$3,'synthetic',$4,FALSE)`, wallet, network, row.address, row.owner)
			require.NoError(t, err)
		}
		addresses, err := (&CryptoProcessor{}).loadWalletAddresses(ctx, network)
		require.NoError(t, err)
		require.Contains(t, addresses, owned, "immutable ownership requires continuous observation through retirement")
		require.NotContains(t, addresses, legacy)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
