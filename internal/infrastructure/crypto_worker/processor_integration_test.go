//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

func TestProcessorLoadsAllTokenContractsAndRefreshesNewWallets(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	errRollback := errors.New("rollback integration fixture")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		networkID, nativeTokenID, tokenAID, tokenBID, walletID := id.New(), id.New(), id.New(), id.New(), id.New()
		suffix := id.Compact(networkID)
		contractA, contractB := "TContractA"+suffix, "TContractB"+suffix
		walletAddress := "TNewWallet" + suffix

		_, err := q.Exec(txCtx, `INSERT INTO cat_blockchain_networks
			(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
			VALUES($1,$2,'Watcher test','tron',$3,'TRX',6)`, networkID, "WATCH-"+suffix, "watch-"+suffix)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_tokens
			(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
			VALUES($1,$5,'Native',$4,'TRX',6,'native',''),
			      ($2,$6,'Token A',$4,'A',6,'TRC-20',$7),
			      ($3,$8,'Token B',$4,'B',6,'TRC-20',$9)`,
			nativeTokenID, tokenAID, tokenBID, networkID, "WATCH-NATIVE-"+suffix, "WATCH-A-"+suffix, contractA, "WATCH-B-"+suffix, contractB)
		require.NoError(t, err)

		processor := &CryptoProcessor{
			log:             logger.Default(),
			addressWatchers: make(map[id.ID][]crypto.ManagedWatcher),
		}
		networks, err := processor.loadNetworks(txCtx)
		require.NoError(t, err)
		var selected *networkInfo
		for i := range networks {
			if networks[i].ref.ID == networkID {
				selected = &networks[i]
				break
			}
		}
		require.NotNil(t, selected)
		contracts := make([]string, 0, len(selected.assets))
		for _, asset := range selected.assets {
			if asset.ContractAddress != "" {
				contracts = append(contracts, asset.ContractAddress)
			}
		}
		require.ElementsMatch(t, []string{contractA, contractB}, contracts)

		watcherA := &addressRefreshWatcher{}
		watcherB := &addressRefreshWatcher{}
		processor.addressWatchers[networkID] = []crypto.ManagedWatcher{watcherA, watcherB}
		require.NoError(t, processor.refreshWalletAddresses(txCtx))
		require.False(t, watcherA.IsMonitoringAddress(walletAddress))

		_, err = q.Exec(txCtx, `INSERT INTO cat_wallets
			(id,code,name,network_id,address,tier,status,vault_key_id,chain_activation_status)
			VALUES($1,$2,'Dynamically added wallet',$3,$4,'pool','free',$5,'active')`,
			walletID, "WATCH-W-"+suffix, networkID, walletAddress, "vault/"+suffix)
		require.NoError(t, err)
		require.NoError(t, processor.refreshWalletAddresses(txCtx))
		require.True(t, watcherA.IsMonitoringAddress(walletAddress))
		require.True(t, watcherB.IsMonitoringAddress(walletAddress))
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}

// A family-neutral watcher double verifies address delivery without coupling the
// processor test to a particular chain's ingestion implementation.
type addressRefreshWatcher struct{ addresses []string }

func (*addressRefreshWatcher) Start(context.Context, crypto.BlockchainEventSink) error { return nil }
func (w *addressRefreshWatcher) ReplaceMonitoredAddresses(a []string) {
	w.addresses = append([]string(nil), a...)
}
func (w *addressRefreshWatcher) IsMonitoringAddress(a string) bool {
	for _, address := range w.addresses {
		if address == a {
			return true
		}
	}
	return false
}
