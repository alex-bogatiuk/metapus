package aml

import (
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

func TestBatchCapacityUsesLifetimeGrossAndSaturatesOnUnwantedExcess(t *testing.T) {
	b := Batch{MaxGross: types.MustCryptoAmount("900719925474099300000000003"), Committed: types.MustCryptoAmount("900719925474099300000000000"), Reserved: types.MustCryptoAmount("2")}
	require.Equal(t, "1", b.AvailableCapacity().String())
	b.Unplanned = types.MustCryptoAmount("100")
	require.Equal(t, "0", b.AvailableCapacity().String())
}

func TestBatchFieldsPreserveLegacyAllocationRequestDigest(t *testing.T) {
	r := InternalAllocationRequest{SweepAttemptID: id.New(), MerchantID: id.New(), NetworkID: id.New(), TokenID: id.New(), SourceWalletID: id.New(), DestinationID: id.New(), Amount: types.MustCryptoAmount("20"), SourceRevision: 1, RiskRevision: 1, ScopeVersion: 1, Class: Low}
	// The old persisted digest covered this exact shape, before batch fields.
	legacy := struct {
		SweepAttemptID, MerchantID, NetworkID, TokenID, SourceWalletID, DestinationID id.ID
		Amount                                                                        types.CryptoAmount
		SourceRevision, RiskRevision, ScopeVersion                                    int64
		Class                                                                         Class
	}{r.SweepAttemptID, r.MerchantID, r.NetworkID, r.TokenID, r.SourceWalletID, r.DestinationID, r.Amount, r.SourceRevision, r.RiskRevision, r.ScopeVersion, r.Class}
	want, err := json.MarshalDeterministic(legacy)
	require.NoError(t, err)
	actual, err := json.MarshalDeterministic(r)
	require.NoError(t, err)
	require.Equal(t, string(want), string(actual))
}
