package crypto

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

func TestBalanceObservationValidatesEvidenceIdentity(t *testing.T) {
	o := BalanceObservation{RunID: id.New(), WalletID: id.New(), NetworkID: id.New(), TokenID: id.New(),
		Kind: BalanceMismatchToken, LedgerAmount: types.NewRawAmountFromInt64(100), ChainAmount: types.NewRawAmountFromInt64(90)}
	require.NoError(t, o.Validate())
	require.Equal(t, "-10", o.Delta().String())
	for _, field := range []*id.ID{&o.RunID, &o.WalletID, &o.NetworkID, &o.TokenID} {
		saved := *field
		*field = id.Nil()
		require.Error(t, o.Validate())
		*field = saved
	}
	o.Kind = "external_outflow"
	require.Error(t, o.Validate(), "outflows require their own evidence and audited workflow")
	o.Kind = BalanceMismatchNative
	o.ChainAmount = o.LedgerAmount
	require.NoError(t, o.Validate())
	require.True(t, o.Delta().IsZero())
}
