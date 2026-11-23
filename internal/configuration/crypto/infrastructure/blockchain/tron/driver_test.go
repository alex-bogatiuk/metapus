package tron

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/token"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

func TestQuoteFeeActivationUsesNativeFeeBound(t *testing.T) {
	nativeTokenID := id.New()
	network := crypto.NetworkRef{
		ID:                    id.New(),
		Code:                  "TRON-NILE",
		Family:                crypto.ChainFamilyTRON,
		NetworkIdentifier:     "nile",
		NativeTokenID:         nativeTokenID,
		NativeDecimals:        6,
		RequiredConfirmations: 20,
		FinalityMode:          crypto.FinalityByConfirmations,
	}
	driver := &Driver{
		network:   network,
		maxFee:    types.NewRawAmountFromInt64(100_000_000),
		nativeFee: types.NewRawAmountFromInt64(2_000_000),
		quoteTTL:  30 * time.Second,
	}

	quote, err := driver.QuoteFee(context.Background(), crypto.TransferIntent{
		BasisType:          "CryptoAccountActivationTask",
		Purpose:            crypto.SigningPurposeActivation,
		BasisID:            id.New(),
		Attempt:            1,
		Network:            network,
		Asset:              crypto.AssetRef{TokenID: nativeTokenID, Standard: token.TokenStandardNative, Decimals: 6},
		SourceWalletID:     id.New(),
		SourceAddress:      "TQv6ey7rE6YuK1rXeRgQpb7WLHaZakWQCK",
		DestinationAddress: "TVAw5xjGeZ97P23FLf1MsKhavERzwrPsfF",
		Amount:             types.ZeroRawAmount(),
	})
	require.NoError(t, err)
	require.Equal(t, types.NewRawAmountFromInt64(2_000_000), quote.EstimatedFee)
	require.Equal(t, quote.EstimatedFee, quote.MaxFee)
}

func TestTRONCapabilitiesAndLagHints(t *testing.T) {
	caps := tronCapabilities()
	require.NoError(t, caps.Validate())
	require.Equal(t, crypto.SequenceNone, caps.SequenceModel)
	require.Equal(t, crypto.CheckpointUnixMillis, caps.CheckpointTimeUnit)
	require.Equal(t, 60, caps.MinWatcherLagSeconds)
	require.False(t, caps.SupportsReplacement)
	require.False(t, caps.RequiresNetworkSequence())

	driver := &Driver{}
	trc20 := driver.LagHints("trc20:TXYZ")
	require.Equal(t, crypto.CheckpointUnixMillis, trc20.TimeUnit)
	require.Equal(t, time.Duration(0), trc20.IndexingLag)
	native := driver.LagHints("native_trx")
	require.Equal(t, time.Duration(0), native.IndexingLag)
}

func TestTRONUsesOnlyNetworkBlockWatchers(t *testing.T) {
	network := crypto.NetworkRef{ID: id.New(), Code: "TRON-NILE", Family: crypto.ChainFamilyTRON, NetworkIdentifier: "nile", NativeTokenID: id.New(), NativeDecimals: 6, RequiredConfirmations: 20, FinalityMode: crypto.FinalityByConfirmations}
	for _, tt := range []struct {
		mode string
		keys []string
	}{{"hybrid", []string{"tron:solidified", "tron:latest"}}, {"solidified", []string{"tron:solidified"}}, {"shadow", []string{"tron:shadow"}}} {
		t.Run(tt.mode, func(t *testing.T) {
			driver := &Driver{network: network, rpc: &RPCPool{rpcGeneration: &rpcGeneration{settings: chainconfig.TRONSettings{IngestionMode: tt.mode}}}, blockRepo: &captureRepoStub{}}
			require.Nil(t, driver.Watchers(), "TRON has no per-asset/indexer watcher factory")
			watchers, err := driver.NetworkWatchers(t.Context(), network, nil, nil)
			require.NoError(t, err)
			keys := []string{}
			for _, w := range watchers {
				block, ok := w.(*blockWatcher)
				require.True(t, ok)
				keys = append(keys, block.key)
			}
			require.Equal(t, tt.keys, keys)
			require.Equal(t, tt.keys, driver.ExpectedWatcherKeys(nil))
		})
	}
}
