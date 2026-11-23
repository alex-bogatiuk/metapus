package crypto_withdrawal

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithdrawalPostingDoesNotSpendPrincipalOrChargeMerchant(t *testing.T) {
	// The merchant is debited by WithdrawalRequest; principal moves at final settlement.
	var doc any = NewCryptoWithdrawal(id.New(), id.New(), id.New(), "external", types.NewRawAmountFromInt64(10))
	_, balance := doc.(cryptoposting.CryptoBalanceMovementSource)
	_, fee := doc.(cryptoposting.CryptoFeeMovementSource)
	_, merchant := doc.(cryptoposting.CryptoMerchantBalanceMovementSource)
	require.False(t, balance)
	require.False(t, fee)
	require.False(t, merchant)
}
