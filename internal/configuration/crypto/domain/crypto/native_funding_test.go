package crypto

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

func TestNativeFundingReceiptIdentity(t *testing.T) {
	f := NativeFunding{ID: id.New(), NetworkID: id.New(), NativeTokenID: id.New(), SourceWalletID: id.New(), DestinationWalletID: id.New(),
		Amount: types.NewRawAmountFromInt64(100), TxHash: "immutable-tx", Status: "broadcast"}
	r := ChainReceipt{NetworkID: f.NetworkID, TxHash: f.TxHash, Status: ReceiptSucceeded, Canonicality: CanonicalityCanonical,
		Finality: FinalitySolidified, Fee: FeeSettlementReport{NativeAsset: AssetRef{TokenID: f.NativeTokenID}}}
	require.NoError(t, f.ValidateReceipt(r))
	for _, mutate := range []func(*ChainReceipt){
		func(r *ChainReceipt) { r.NetworkID = id.New() },
		func(r *ChainReceipt) { r.TxHash = "other-tx" },
		func(r *ChainReceipt) { r.Fee.NativeAsset.TokenID = id.New() },
		func(r *ChainReceipt) { r.Finality = FinalityProvisional },
		func(r *ChainReceipt) { r.Status = ReceiptPending },
		func(r *ChainReceipt) { r.Canonicality = CanonicalityOrphaned },
	} {
		candidate := r
		mutate(&candidate)
		require.Error(t, f.ValidateReceipt(candidate))
	}
}
