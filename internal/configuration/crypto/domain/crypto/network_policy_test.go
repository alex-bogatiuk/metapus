package crypto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestNetworkPolicyEnabledIsFailClosed(t *testing.T) {
	networkID := id.New()
	base := NetworkPolicy{
		NetworkID: networkID, Version: 1,
		RetryDelay: time.Second, RPCFailureThreshold: 1, RPCOpenDuration: time.Second,
	}
	require.NoError(t, base.Validate())
	base.Enabled = true
	require.Error(t, base.Validate())
	base.PerTransactionMaxFee = types.NewRawAmountFromInt64(100)
	base.DailyNetworkFeeBudget = types.NewRawAmountFromInt64(1_000)
	base.FeeFunding = FeeFundingPolicy{Mode: FeeFundingJIT, MinSourceWalletResidual: types.NewRawAmountFromInt64(1), MaxFundingAmount: types.NewRawAmountFromInt64(101), DailyBudget: types.NewRawAmountFromInt64(10_000)}
	require.NoError(t, base.Validate())
	require.NoError(t, base.ValidateForCapabilities(evmWalletPoolCapabilities(), true))
}

func TestNetworkPolicyEnabledRequiresJITFeeCapacity(t *testing.T) {
	networkID := id.New()
	policy := NetworkPolicy{
		NetworkID: networkID, Enabled: true, Version: 1,
		PerTransactionMaxFee:  types.NewRawAmountFromInt64(100),
		DailyNetworkFeeBudget: types.NewRawAmountFromInt64(1),
		FeeFunding: FeeFundingPolicy{Mode: FeeFundingJIT, DailyBudget: types.NewRawAmountFromInt64(101),
			MinSourceWalletResidual: types.NewRawAmountFromInt64(1), MaxFundingAmount: types.NewRawAmountFromInt64(101)},
		RetryDelay: time.Second, RPCFailureThreshold: 1, RPCOpenDuration: time.Second,
	}

	require.ErrorContains(t, policy.ValidateForCapabilities(evmWalletPoolCapabilities(), true), "daily JIT funding budget")
	policy.FeeFunding.DailyBudget = types.NewRawAmountFromInt64(102)
	require.NoError(t, policy.ValidateForCapabilities(evmWalletPoolCapabilities(), true))
}

func TestNetworkPolicyBoundFeeQuoteRejectsMaxFeeAboveRuntimeCap(t *testing.T) {
	policy := NetworkPolicy{
		Enabled:              true,
		PerTransactionMaxFee: types.NewRawAmountFromInt64(20),
	}
	quote := FeeQuote{
		EstimatedFee: types.NewRawAmountFromInt64(15),
		MaxFee:       types.NewRawAmountFromInt64(100),
	}

	_, err := policy.BoundFeeQuote(quote)
	require.ErrorContains(t, err, "maximum network fee")
	require.Equal(t, types.NewRawAmountFromInt64(100), quote.MaxFee, "fee quotes are immutable by value")
}

func TestNetworkPolicyBoundFeeQuoteRejectsEstimateAboveRuntimeCap(t *testing.T) {
	policy := NetworkPolicy{
		Enabled:              true,
		PerTransactionMaxFee: types.NewRawAmountFromInt64(20),
	}
	quote := FeeQuote{
		EstimatedFee: types.NewRawAmountFromInt64(21),
		MaxFee:       types.NewRawAmountFromInt64(100),
	}

	_, err := policy.BoundFeeQuote(quote)
	require.ErrorContains(t, err, "exceeds per-transaction policy cap")
}

func TestNetworkPolicyBoundFeeQuoteAllowsRentalRequiredContractQuoteAboveBurnCap(t *testing.T) {
	policy := NetworkPolicy{
		Enabled:              true,
		PerTransactionMaxFee: types.NewRawAmountFromInt64(15_000_000),
		ExternalResources: &ExternalResourcePolicy{
			Strategy: ResourceRentalRequired,
		},
	}
	quote := FeeQuote{
		EstimatedFee:         types.NewRawAmountFromInt64(15_566_460),
		MaxFee:               types.NewRawAmountFromInt64(15_566_460),
		ResourceRequirements: []ResourceRequirement{{Name: "energy", Quantity: 37_063}},
	}

	bounded, err := policy.BoundFeeQuote(quote)
	require.NoError(t, err)
	require.Equal(t, quote.MaxFee, bounded.MaxFee)
}

func TestNetworkRuntimeStatusReadyRequiresFreshHealthyRoleCoverage(t *testing.T) {
	now := time.Now().UTC()
	status := NetworkRuntimeStatus{
		SignerReady: true, ResourceProviderReady: true, ObservedAt: now,
		RPCEndpoints: []RuntimeEndpointStatus{
			{Role: "indexer", State: "closed"},
			{Role: "fullnode", State: "closed"},
			{Role: "solidity", State: "closed"},
		},
	}
	capabilities := tronWalletPoolCapabilities()
	require.True(t, status.Ready(now, capabilities, true))

	status.RPCEndpoints[2].State = "lagging"
	require.False(t, status.Ready(now, capabilities, true))
	status.RPCEndpoints[2].State = "closed"
	status.ResourceProviderReady = false
	require.True(t, status.Ready(now, capabilities, false), "burn-only policy does not require a rental provider")
	require.False(t, status.Ready(now, capabilities, true))
	status.ResourceProviderReady = true
	require.False(t, status.Ready(now.Add(61*time.Second), capabilities, true))
}

func TestNetworkPolicyApplyPatchCopiesProviderAccountIDs(t *testing.T) {
	oldID, newID, replacementID := id.New(), id.New(), id.New()
	original := NetworkPolicy{ExternalResources: &ExternalResourcePolicy{ProviderAccountIDs: []id.ID{oldID}}}
	patched := original.ApplyPatch(NetworkPolicyPatch{})
	patched.ExternalResources.ProviderAccountIDs[0] = replacementID
	require.Equal(t, oldID, original.ExternalResources.ProviderAccountIDs[0])

	input := &ExternalResourcePolicy{ProviderAccountIDs: []id.ID{newID}}
	patched = original.ApplyPatch(NetworkPolicyPatch{ExternalResources: input})
	input.ProviderAccountIDs[0] = replacementID
	require.Equal(t, newID, patched.ExternalResources.ProviderAccountIDs[0])
}
