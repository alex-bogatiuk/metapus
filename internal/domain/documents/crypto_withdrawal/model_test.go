package crypto_withdrawal

import (
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithdrawalPostingDoesNotSpendPrincipalOrChargeMerchant(t *testing.T) {
	// The merchant is debited by WithdrawalRequest; principal moves at final settlement.
	var doc any = NewCryptoWithdrawal(id.New(), id.New(), id.New(), "external", types.NewCryptoAmountFromInt64(10))
	_, balance := doc.(posting.CryptoBalanceMovementSource)
	_, fee := doc.(posting.CryptoFeeMovementSource)
	_, merchant := doc.(posting.CryptoMerchantBalanceMovementSource)
	require.False(t, balance)
	require.False(t, fee)
	require.False(t, merchant)
}
