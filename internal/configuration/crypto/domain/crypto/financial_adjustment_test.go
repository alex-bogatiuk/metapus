package crypto

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"

	"github.com/stretchr/testify/require"
)

func TestAdjustmentDecisionProtectsMakerCheckerAndGeneration(t *testing.T) {
	maker, checker := id.New(), id.New()
	base := FinancialAdjustment{Status: "pending_approval", DecisionMode: "two_person", MakerID: maker, Mode: "payment_reversal", PostedVersion: 3}
	settled := AdjustmentPaymentBasis{Status: "settled", Posted: true, Version: 3}
	_, err := base.PlanPosting(maker, "reason", false, settled)
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeForbidden, appErr.Code)
	plan, err := base.PlanPosting(checker, "reason", false, settled)
	require.NoError(t, err)
	require.True(t, plan.CompensatePayment)
	for _, basis := range []AdjustmentPaymentBasis{
		{Status: "confirmed", Posted: true, Version: 3},
		{Status: "settled", Posted: false, Version: 3},
		{Status: "settled", Posted: true, Version: 4},
	} {
		_, err := base.PlanPosting(checker, "reason", false, basis)
		require.Error(t, err)
	}
	base.Status = "posted"
	require.Error(t, base.CanDecide(checker, false))
	base.Status, base.DecisionMode = "pending_approval", "direct"
	require.Error(t, base.CanDecide(checker, false))
	require.NoError(t, base.CanDecide(maker, true))
}

func TestAdjustmentWalletPlanRequiresBasisAndChoosesVariance(t *testing.T) {
	wallet, token := id.New(), id.New()
	a := FinancialAdjustment{Status: "pending_approval", DecisionMode: "direct", Mode: "wallet_balance", WalletID: &wallet, TokenID: &token, Direction: "receipt"}
	plan, err := a.PlanPosting(id.New(), "reason", true, AdjustmentPaymentBasis{})
	require.NoError(t, err)
	require.Equal(t, "surplus", plan.VarianceType)
	require.False(t, plan.CompensatePayment)
	a.Direction = "expense"
	plan, err = a.PlanPosting(id.New(), "reason", true, AdjustmentPaymentBasis{})
	require.NoError(t, err)
	require.Equal(t, "shortage", plan.VarianceType)
	a.TokenID = nil
	_, err = a.PlanPosting(id.New(), "reason", true, AdjustmentPaymentBasis{})
	require.Error(t, err)
}
