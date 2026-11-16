package crypto

import (
	"metapus/internal/core/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeeAdmissionUsesExactLimitsForPrincipalAndFee(t *testing.T) {
	cap := types.MustRawAmount("900719925474099300000000003")
	policy := NetworkPolicy{Enabled: true, PerTransactionMaxFee: cap, DailyNetworkFeeBudget: cap}
	committed := cap.Sub(types.NewRawAmountFromInt64(3))
	remaining, err := policy.CheckDailyFeeCommitment(FeeStrategyNativeBurn, committed, types.NewRawAmountFromInt64(3))
	require.NoError(t, err)
	require.True(t, remaining.IsZero())
	_, err = policy.CheckDailyFeeCommitment(FeeStrategyNativeBurn, committed, types.NewRawAmountFromInt64(4))
	require.ErrorIs(t, err, ErrFeeBudgetExceeded)
	require.NoError(t, policy.CheckFeeCap(FeeStrategyNativeBurn, cap))
	require.ErrorIs(t, policy.CheckFeeCap(FeeStrategyNativeBurn, cap.Add(types.NewRawAmountFromInt64(1))), ErrFeeBudgetExceeded)
	policy.OperationalHold = true
	require.ErrorIs(t, policy.CheckFeeCap(FeeStrategyResourceRental, cap), ErrNetworkOperationalHold)
	policy.Enabled = false
	require.ErrorIs(t, policy.CheckFeeCap(FeeStrategyNativeBurn, cap), ErrNetworkPolicyDisabled)
}

func TestFeeAdmissionCannotHideAMLOperatingDeficitOrSpendResidual(t *testing.T) {
	amount := types.NewRawAmountFromInt64
	policy := NetworkPolicy{FeeFunding: FeeFundingPolicy{MinSourceWalletResidual: amount(5), MinFeeWalletBalance: amount(10)}}
	balance := EffectiveFeeBalance(amount(100), amount(14), true)
	require.Equal(t, "14", balance.String())
	require.ErrorIs(t, policy.CheckFeeWalletBalance("network_fee", balance, amount(2), amount(3)), ErrFeeBalanceResidual)
	require.NoError(t, policy.CheckFeeWalletBalance("liquidity_hot", balance, amount(2), amount(3)))
	require.NoError(t, policy.CheckFeeWalletBalance("network_fee", amount(15), amount(2), amount(3)))
	require.Equal(t, "100", EffectiveFeeBalance(amount(100), amount(500), true).String())
	require.ErrorIs(t, (FeeReservation{Status: FeeReservationHeld}).CheckReusable(), ErrFeeReservationHeld)
	require.Error(t, (FeeReservation{Status: FeeReservationReleased}).CheckReusable())
}
