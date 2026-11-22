//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/tenant"
)

func withNetworkCatalog(t *testing.T, fn func(context.Context, *postgres.TxManager)) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn, "a disposable integration database is required")
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(context.Background(), txm)
	rollback := errors.New("rollback network catalog fixture")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		fn(txCtx, txm)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}

func TestProcessorNetworkCatalogLoadsEveryNetwork(t *testing.T) {
	withNetworkCatalog(t, func(ctx context.Context, txm *postgres.TxManager) {
		const networkCount = 1005
		prefix := "CAT-N-" + id.Compact(id.New())
		q := txm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `INSERT INTO cat_blockchain_networks
			(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
			SELECT gen_random_uuid_v7(),$1||'-'||g,'Large catalog network','tron',$1||'-'||g,'TRX',6
			FROM generate_series(1,$2::int) g`, prefix, networkCount)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO cat_tokens
			(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
			SELECT gen_random_uuid_v7(),code||'-N','Native',id,'TRX',6,'native',''
			FROM cat_blockchain_networks WHERE code LIKE $1||'-%'`, prefix)
		require.NoError(t, err)

		processor := &CryptoProcessor{}
		networks, err := processor.loadNetworks(ctx)
		require.NoError(t, err)
		found := make(map[id.ID]struct{}, networkCount)
		for _, network := range networks {
			if !strings.HasPrefix(network.ref.Code, prefix+"-") {
				continue
			}
			_, duplicate := found[network.ref.ID]
			require.False(t, duplicate, "network must occur once: %s", network.ref.ID)
			found[network.ref.ID] = struct{}{}
			require.Len(t, network.assets, 1)
			require.Equal(t, network.ref.NativeTokenID, network.assets[0].TokenID)
			require.Equal(t, crypto.ChainFamilyTRON, network.ref.Family)
		}
		require.Equal(t, networkCount, len(found), "runtime discovery must not silently omit networks beyond a global row limit")

		// Invalid configuration beyond the former limit must fail the complete
		// load, not allow startup with a seemingly valid partial catalog.
		invalidID := id.New()
		_, err = q.Exec(ctx, `INSERT INTO cat_blockchain_networks
			(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
			VALUES($1,$2||'-bad','Missing native token','tron',$2||'-bad','TRX',6)`, invalidID, prefix)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO cat_tokens
			(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
			VALUES(gen_random_uuid_v7(),$2||'-bad-T','Contract without native',$1,'BAD',6,'TRC-20',$2||'-bad-contract')`, invalidID, prefix)
		require.NoError(t, err)
		networks, err = processor.loadNetworks(ctx)
		require.ErrorContains(t, err, "network and native token IDs are required")
		require.Nil(t, networks, "invalid configuration must not expose a partial runtime catalog")
	})
}

func TestProcessorNetworkCatalogLoadsEveryAsset(t *testing.T) {
	for _, family := range []crypto.ChainFamily{crypto.ChainFamilyTRON, crypto.ChainFamilyEVM} {
		nativeSymbol, standard, decimals := "TRX", "TRC-20", 6
		if family == crypto.ChainFamilyEVM {
			nativeSymbol, standard, decimals = "ETH", "ERC-20", 18
		}
		for _, active := range []bool{true, false} {
			for _, nativeLast := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/active=%t/nativeLast=%t", family, active, nativeLast), func(t *testing.T) {
					withNetworkCatalog(t, func(ctx context.Context, txm *postgres.TxManager) {
						const contractCount = 1005
						networkID, nativeID := id.New(), id.New()
						prefix := "CAT-A-" + id.Compact(networkID)
						identifier := prefix
						if family == crypto.ChainFamilyEVM {
							identifier = "313370001"
						}
						q := txm.GetQuerier(ctx)
						_, err := q.Exec(ctx, `INSERT INTO cat_blockchain_networks
						(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
						VALUES($1,$2,'Large contract catalog',$4,$5,$6,$7,$3)`, networkID, prefix, active, family, identifier, nativeSymbol, decimals)
						require.NoError(t, err)
						nativeCode := prefix + "-0-native"
						if nativeLast {
							nativeCode = prefix + "-z-native"
						}
						_, err = q.Exec(ctx, `INSERT INTO cat_tokens
						(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
						VALUES($1,$2,'Native',$3,$4,$5,'native','')`, nativeID, nativeCode, networkID, nativeSymbol, decimals)
						require.NoError(t, err)
						_, err = q.Exec(ctx, `INSERT INTO cat_tokens
						(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
						SELECT gen_random_uuid_v7(),$2||'-1-'||lpad(g::text,4,'0'),'Contract '||g,$1,'TKN'||g,$4,$5,$2||'-contract-'||g
						FROM generate_series(1,$3::int) g`, networkID, prefix, contractCount, decimals, standard)
						require.NoError(t, err)
						_, err = q.Exec(ctx, `INSERT INTO cat_tokens
						(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address,is_active,deletion_mark)
						VALUES(gen_random_uuid_v7(),$2||'-inactive','Inactive',$1,'OFF',$3,$4,$2||'-inactive',FALSE,FALSE),
						      (gen_random_uuid_v7(),$2||'-deleted','Deleted',$1,'DEL',$3,$4,$2||'-deleted',TRUE,TRUE)`, networkID, prefix, decimals, standard)
						require.NoError(t, err)

						assertComplete := func(network networkInfo) {
							t.Helper()
							require.Equal(t, networkID, network.ref.ID)
							require.Equal(t, nativeID, network.ref.NativeTokenID)
							require.Equal(t, family, network.ref.Family)
							require.Equal(t, decimals, network.ref.NativeDecimals)
							require.Equal(t, contractCount+1, len(network.assets))
							contracts := make(map[string]struct{}, contractCount)
							for _, asset := range network.assets {
								if asset.TokenID != nativeID {
									require.Equal(t, standard, string(asset.Standard))
									require.Equal(t, decimals, asset.Decimals)
									_, duplicate := contracts[asset.ContractAddress]
									require.False(t, duplicate, "duplicate contract: %s", asset.ContractAddress)
									contracts[asset.ContractAddress] = struct{}{}
								}
							}
							require.Equal(t, contractCount, len(contracts))
							for i := 1; i <= contractCount; i++ {
								contract := fmt.Sprintf("%s-contract-%d", prefix, i)
								_, exists := contracts[contract]
								require.True(t, exists, "missing contract: %s", contract)
							}
						}
						processor := &CryptoProcessor{}
						// Worker discovery excludes inactive networks but must load every
						// active contract regardless of native-token sort position.
						networks, err := processor.loadNetworks(ctx)
						require.NoError(t, err)
						found := false
						for _, network := range networks {
							if network.ref.ID == networkID {
								found = true
								assertComplete(network)
							}
						}
						require.Equal(t, active, found)
						// Deployment preflight also requires the full contract set,
						// including when the selected network has not been activated yet.
						selected, err := processor.loadNetworksFiltered(ctx, false, prefix)
						require.NoError(t, err)
						require.Len(t, selected, 1)
						assertComplete(selected[0])
						selected, err = processor.loadNetworksFiltered(ctx, true, prefix)
						require.NoError(t, err)
						if active {
							require.Len(t, selected, 1)
							assertComplete(selected[0])
						} else {
							require.Empty(t, selected)
						}
						selected, err = processor.loadNetworksFiltered(ctx, false, prefix+"-missing")
						require.NoError(t, err)
						require.Empty(t, selected)
					})
				})
			}
		}
	}
}
