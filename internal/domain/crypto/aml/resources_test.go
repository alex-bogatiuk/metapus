package aml

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestOperatingResourceCapUsesExactNativeUnits(t *testing.T) {
	r := ResourceRequirement{Funding: types.MustCryptoAmount("900719925474099300000000000000000000"), MaxFee: types.MustCryptoAmount("3")}
	require.Equal(t, "900719925474099300000000000000000003", r.Total().String())
	require.NoError(t, r.ValidateAvailable(r.Total()))
	require.ErrorContains(t, r.ValidateAvailable(types.MustCryptoAmount("900719925474099300000000000000000002")), "merchant principal cannot fund gas")
	require.Error(t, (ResourceRequirement{}).ValidateAvailable(types.MustCryptoAmount("100")))

	rentalCovered := ResourceRequirement{MaxFee: types.MustCryptoAmount("27300000"), RentalCovered: true}
	require.NoError(t, rentalCovered.ValidateAvailable(types.ZeroCryptoAmount()))
	require.NoError(t, rentalCovered.ValidateAvailable(types.MustCryptoAmount("1000000")))

	rentalWithFunding := ResourceRequirement{Funding: types.MustCryptoAmount("500"), MaxFee: types.MustCryptoAmount("27300000"), RentalCovered: true}
	require.NoError(t, rentalWithFunding.ValidateAvailable(types.MustCryptoAmount("500")))
	require.ErrorContains(t, rentalWithFunding.ValidateAvailable(types.MustCryptoAmount("499")), "merchant principal cannot fund gas")
}

func TestOperatingResourceReserveRequiresCurrentOwnedLowOrMedium(t *testing.T) {
	r := ResourceBudgetRequest{FeeReservationID: id.New(), MerchantID: id.New(), ResourceRevision: 1, RiskRevision: 1, ScopeVersion: 1, Class: Low}
	require.NoError(t, r.Validate())
	r.Class = Medium
	require.NoError(t, r.Validate())
	for _, class := range []Class{High, Unknown, ""} {
		r.Class = class
		require.Error(t, r.Validate())
	}
}

func TestOperatingResourceHighRequiresExplicitExportContext(t *testing.T) {
	r := ResourceBudgetRequest{FeeReservationID: id.New(), MerchantID: id.New(), ResourceRevision: 1, RiskRevision: 1, ScopeVersion: 1, RiskExportAccepted: true}
	for _, class := range []Class{High, Unknown} {
		r.Class = class
		require.NoError(t, r.Validate())
	}
	r.Class = "invalid"
	require.Error(t, r.Validate())
}
