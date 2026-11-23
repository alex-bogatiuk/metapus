package crypto

import (
	"context"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

const MerchantPoolActor = "aml-pool-controller"

type MerchantPoolPolicy struct {
	ID, MerchantID, NetworkID                              id.ID
	Role                                                   string
	TokenID                                                *id.ID
	RiskClass                                              string
	Enabled                                                bool
	AutomaticBaseline                                      bool
	Version                                                int64
	LowWatermark, TargetReady, MaxTotal, MaxProvisionBatch int
}

type MerchantPoolPolicyRequest struct {
	Policy          MerchantPoolPolicy
	ExpectedVersion int64
	Actor           string
}

func (r MerchantPoolPolicyRequest) Validate() error {
	p := r.Policy
	if id.IsNil(p.ID) || id.IsNil(p.MerchantID) || id.IsNil(p.NetworkID) || r.ExpectedVersion < 0 || strings.TrimSpace(r.Actor) == "" || p.LowWatermark < 0 || p.LowWatermark >= p.TargetReady || p.TargetReady > p.MaxTotal || p.MaxTotal > 100000 || p.MaxProvisionBatch < 1 || p.MaxProvisionBatch > 50 {
		return apperror.NewValidation("merchant pool requires immutable scope, bounded watermarks, current version and actor")
	}
	switch p.Role {
	case "liquidity_hot":
		if p.TokenID == nil || id.IsNil(*p.TokenID) || (p.RiskClass != "low" && p.RiskClass != "medium") {
			return apperror.NewValidation("collector pool requires token and low/medium class")
		}
	case "merchant_pool", "network_fee":
		if p.TokenID != nil || p.RiskClass != "" {
			return apperror.NewValidation("deposit and fee pools have network scope")
		}
		if p.Role == "network_fee" && (p.TargetReady != 1 || p.LowWatermark != 0 || p.MaxProvisionBatch != 1) {
			return apperror.NewValidation("fee pool targets one active wallet")
		}
	default:
		return apperror.NewValidation("unsupported merchant pool role")
	}
	return nil
}

type MerchantPoolStats struct{ Ready, Pending, Queued, Total, Started int }

// PlanMerchantPoolRefill treats unfinished preflight as committed capacity.
// Both the scope's lifetime budget and the shared network budget are finite.
func PlanMerchantPoolRefill(p MerchantPoolPolicy, s MerchantPoolStats, networkRemaining int) int {
	if !p.Enabled || s.Ready > p.LowWatermark || networkRemaining <= 0 {
		return 0
	}
	count := p.TargetReady - s.Ready - s.Pending - s.Queued
	for _, cap := range []int{p.MaxTotal - s.Total, p.MaxProvisionBatch, networkRemaining} {
		if cap < count {
			count = cap
		}
	}
	if count < 0 {
		return 0
	}
	return count
}

type MerchantPoolProvisionQueue interface {
	ClaimNextMerchantPoolProvision(context.Context, string, time.Duration) (MerchantWalletProvision, bool, error)
}
