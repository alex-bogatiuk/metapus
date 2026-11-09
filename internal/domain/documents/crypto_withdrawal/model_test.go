package crypto_withdrawal

import (
	"context"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"testing"
)

func TestWithdrawalV2BroadcastDoesNotSpendPrincipalOrChargeMerchant(t *testing.T) {
	d := NewCryptoWithdrawal(id.New(), id.New(), id.New(), "external", types.NewCryptoAmountFromInt64(23))
	d.NetworkFee = types.NewCryptoAmountFromInt64(2)
	principal, err := d.GenerateCryptoBalanceMovements(context.Background())
	require.NoError(t, err)
	require.Empty(t, principal)
	fees, err := d.GenerateCryptoFeeMovements(context.Background())
	require.NoError(t, err)
	require.Empty(t, fees)
	merchant, err := d.GenerateCryptoMerchantBalanceMovements(context.Background())
	require.NoError(t, err)
	require.Empty(t, merchant)
	d.AccountingModelVersion = 1
	principal, err = d.GenerateCryptoBalanceMovements(context.Background())
	require.NoError(t, err)
	require.Len(t, principal, 1)
}
