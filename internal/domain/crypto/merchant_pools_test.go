package crypto

import (
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"testing"
)

func TestMerchantPoolRefillReservesPreflightQueueAndFiniteBudgets(t *testing.T) {
	p := MerchantPoolPolicy{Enabled: true, LowWatermark: 1, TargetReady: 5, MaxTotal: 10, MaxProvisionBatch: 3}
	for _, tc := range []struct {
		name          string
		stats         MerchantPoolStats
		network, want int
	}{
		{"ready", MerchantPoolStats{Ready: 2, Total: 2}, 10, 0},
		{"bounded round", MerchantPoolStats{}, 10, 3},
		{"network budget", MerchantPoolStats{}, 1, 1},
		{"unfinished preflight", MerchantPoolStats{Pending: 3, Queued: 2, Total: 5}, 10, 0},
		{"pool budget", MerchantPoolStats{Ready: 1, Total: 9}, 10, 1},
		{"exhausted", MerchantPoolStats{Total: 10}, 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, PlanMerchantPoolRefill(p, tc.stats, tc.network)) })
	}
	p.Enabled = false
	require.Zero(t, PlanMerchantPoolRefill(p, MerchantPoolStats{}, 10))
}

func TestMerchantPoolPolicyScopeAndFeeWalletLimits(t *testing.T) {
	r := MerchantPoolPolicyRequest{Policy: MerchantPoolPolicy{ID: id.New(), MerchantID: id.New(), NetworkID: id.New(), Role: "merchant_pool", TargetReady: 2, MaxTotal: 5, MaxProvisionBatch: 2}, Actor: "operator"}
	require.NoError(t, r.Validate())
	r.Policy.Role = "liquidity_hot"
	require.Error(t, r.Validate())
	token := id.New()
	r.Policy.TokenID = &token
	r.Policy.RiskClass = "low"
	require.NoError(t, r.Validate())
	r.Policy.RiskClass = "high"
	require.Error(t, r.Validate())
	r.Policy.Role = "network_fee"
	r.Policy.TokenID = nil
	r.Policy.RiskClass = ""
	require.Error(t, r.Validate())
	r.Policy.TargetReady = 1
	r.Policy.MaxProvisionBatch = 1
	require.NoError(t, r.Validate())
}
