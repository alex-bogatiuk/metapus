package aml

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

func TestOperatingResourceCapUsesExactNativeUnits(t *testing.T) {
	r := ResourceRequirement{Funding: types.MustRawAmount("900719925474099300000000000000000000"), MaxFee: types.MustRawAmount("3")}
	require.Equal(t, "900719925474099300000000000000000003", r.Total().String())
	require.NoError(t, r.ValidateAvailable(r.Total()))
	require.ErrorContains(t, r.ValidateAvailable(types.MustRawAmount("900719925474099300000000000000000002")), "merchant principal cannot fund gas")
	require.Error(t, (ResourceRequirement{}).ValidateAvailable(types.MustRawAmount("100")))

	rentalCovered := ResourceRequirement{MaxFee: types.MustRawAmount("27300000"), RentalCovered: true}
	require.NoError(t, rentalCovered.ValidateAvailable(types.ZeroRawAmount()))
	require.NoError(t, rentalCovered.ValidateAvailable(types.MustRawAmount("1000000")))

	rentalWithFunding := ResourceRequirement{Funding: types.MustRawAmount("500"), MaxFee: types.MustRawAmount("27300000"), RentalCovered: true}
	require.NoError(t, rentalWithFunding.ValidateAvailable(types.MustRawAmount("500")))
	require.ErrorContains(t, rentalWithFunding.ValidateAvailable(types.MustRawAmount("499")), "merchant principal cannot fund gas")
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
