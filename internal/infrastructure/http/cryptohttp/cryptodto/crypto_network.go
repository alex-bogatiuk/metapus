package cryptodto

import (
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

type CryptoNetworkListResponse struct {
	Items []CryptoNetworkStateResponse `json:"items"`
}

type CryptoShadowControlResponse = crypto.ShadowControlState

type SetCryptoShadowControlRequest struct {
	Version int    `json:"version" binding:"required,gt=0"`
	Paused  *bool  `json:"paused" binding:"required"`
	Reason  string `json:"reason" binding:"required,min=3,max=500"`
}

// SetCryptoNetworkWatchingRequest starts or stops reading a network.
type SetCryptoNetworkWatchingRequest struct {
	Watching *bool  `json:"watching" binding:"required"`
	Reason   string `json:"reason" binding:"required,min=3,max=500"`
}

type FeeFundingPolicyRequest struct {
	Mode                         string          `json:"mode" binding:"required"`
	DailyBudgetMinor             types.RawAmount `json:"dailyBudgetMinor"`
	MinFeeWalletBalanceMinor     types.RawAmount `json:"minFeeWalletBalanceMinor"`
	MinSourceWalletResidualMinor types.RawAmount `json:"minSourceWalletResidualMinor"`
	MaxFundingAmountMinor        types.RawAmount `json:"maxFundingAmountMinor"`
}

type ExternalResourcePolicyRequest struct {
	Strategy                 string   `json:"strategy" binding:"required"`
	ProviderAccountIDs       []string `json:"providerAccountIds"`
	ProviderFailureThreshold int      `json:"providerFailureThreshold" binding:"required,gt=0"`
	ProviderOpenSeconds      int      `json:"providerOpenSeconds" binding:"required,gt=0"`
}

type UpdateCryptoNetworkPolicyRequest struct {
	Version                    int                            `json:"version" binding:"required,gt=0"`
	Enabled                    *bool                          `json:"enabled"`
	PerTransactionMaxFeeMinor  *types.RawAmount               `json:"perTransactionMaxFeeMinor"`
	DailyNetworkFeeBudgetMinor *types.RawAmount               `json:"dailyNetworkFeeBudgetMinor"`
	FeeFunding                 *FeeFundingPolicyRequest       `json:"feeFunding"`
	ExternalResources          *ExternalResourcePolicyRequest `json:"externalResources"`
	ClearExternalResources     bool                           `json:"clearExternalResources"`
	RetryDelaySeconds          *int                           `json:"retryDelaySeconds"`
	RPCFailureThreshold        *int                           `json:"rpcFailureThreshold"`
	RPCOpenSeconds             *int                           `json:"rpcOpenSeconds"`
}

type FeeFundingPolicyResponse struct {
	Mode                         crypto.FeeFundingMode `json:"mode"`
	DailyBudgetMinor             types.RawAmount       `json:"dailyBudgetMinor"`
	MinFeeWalletBalanceMinor     types.RawAmount       `json:"minFeeWalletBalanceMinor"`
	MinSourceWalletResidualMinor types.RawAmount       `json:"minSourceWalletResidualMinor"`
	MaxFundingAmountMinor        types.RawAmount       `json:"maxFundingAmountMinor"`
}

type ExternalResourcePolicyResponse struct {
	Strategy                 crypto.ResourceStrategyPolicy `json:"strategy"`
	ProviderAccountIDs       []id.ID                       `json:"providerAccountIds"`
	ProviderFailureThreshold int                           `json:"providerFailureThreshold"`
	ProviderOpenSeconds      int64                         `json:"providerOpenSeconds"`
}

type CryptoNetworkPolicyResponse struct {
	NetworkID                  id.ID                           `json:"networkId"`
	Enabled                    bool                            `json:"enabled"`
	PerTransactionMaxFeeMinor  types.RawAmount                 `json:"perTransactionMaxFeeMinor"`
	DailyNetworkFeeBudgetMinor types.RawAmount                 `json:"dailyNetworkFeeBudgetMinor"`
	FeeFunding                 FeeFundingPolicyResponse        `json:"feeFunding"`
	ExternalResources          *ExternalResourcePolicyResponse `json:"externalResources,omitempty"`
	RetryDelaySeconds          int64                           `json:"retryDelaySeconds"`
	RPCFailureThreshold        int                             `json:"rpcFailureThreshold"`
	RPCOpenSeconds             int64                           `json:"rpcOpenSeconds"`
	Version                    int                             `json:"version"`
	UpdatedBy                  string                          `json:"updatedBy"`
	CreatedAt                  time.Time                       `json:"createdAt"`
	UpdatedAt                  time.Time                       `json:"updatedAt"`
}

type CryptoNetworkStateResponse struct {
	Network      crypto.NetworkRef              `json:"network"`
	Watching     bool                           `json:"watching"`
	Capabilities crypto.ChainFamilyCapabilities `json:"capabilities"`
	Policy       CryptoNetworkPolicyResponse    `json:"policy"`
	Runtime      crypto.NetworkRuntimeStatus    `json:"runtime"`
}

func CryptoNetworkStateFromDomain(state crypto.NetworkPolicyState) CryptoNetworkStateResponse {
	policy := state.Policy
	response := CryptoNetworkStateResponse{
		Network: state.Network, Watching: state.Watching, Capabilities: state.Capabilities,
		Policy: CryptoNetworkPolicyResponse{
			NetworkID: policy.NetworkID, Enabled: policy.Enabled,
			PerTransactionMaxFeeMinor:  policy.PerTransactionMaxFee,
			DailyNetworkFeeBudgetMinor: policy.DailyNetworkFeeBudget,
			FeeFunding: FeeFundingPolicyResponse{
				Mode: policy.FeeFunding.Mode, DailyBudgetMinor: policy.FeeFunding.DailyBudget,
				MinFeeWalletBalanceMinor:     policy.FeeFunding.MinFeeWalletBalance,
				MinSourceWalletResidualMinor: policy.FeeFunding.MinSourceWalletResidual,
				MaxFundingAmountMinor:        policy.FeeFunding.MaxFundingAmount,
			},
			RetryDelaySeconds: int64(policy.RetryDelay / time.Second), RPCFailureThreshold: policy.RPCFailureThreshold,
			RPCOpenSeconds: int64(policy.RPCOpenDuration / time.Second), Version: policy.Version,
			UpdatedBy: policy.UpdatedBy, CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt,
		},
		Runtime: state.Runtime,
	}
	if policy.ExternalResources != nil {
		response.Policy.ExternalResources = &ExternalResourcePolicyResponse{
			Strategy:                 policy.ExternalResources.Strategy,
			ProviderAccountIDs:       append([]id.ID(nil), policy.ExternalResources.ProviderAccountIDs...),
			ProviderFailureThreshold: policy.ExternalResources.ProviderFailureThreshold,
			ProviderOpenSeconds:      int64(policy.ExternalResources.ProviderOpenDuration / time.Second),
		}
	}
	return response
}

func (r UpdateCryptoNetworkPolicyRequest) ToDomain() (crypto.NetworkPolicyPatch, error) {
	patch := crypto.NetworkPolicyPatch{
		Version: r.Version, Enabled: r.Enabled, PerTransactionMaxFee: r.PerTransactionMaxFeeMinor,
		DailyNetworkFeeBudget: r.DailyNetworkFeeBudgetMinor, ClearExternalResources: r.ClearExternalResources,
		RPCFailureThreshold: r.RPCFailureThreshold,
	}
	if r.FeeFunding != nil {
		funding := crypto.FeeFundingPolicy{
			Mode: crypto.FeeFundingMode(r.FeeFunding.Mode), DailyBudget: r.FeeFunding.DailyBudgetMinor,
			MinFeeWalletBalance:     r.FeeFunding.MinFeeWalletBalanceMinor,
			MinSourceWalletResidual: r.FeeFunding.MinSourceWalletResidualMinor,
			MaxFundingAmount:        r.FeeFunding.MaxFundingAmountMinor,
		}

		patch.FeeFunding = &funding
	}
	if r.ExternalResources != nil {
		accountIDs := make([]id.ID, 0, len(r.ExternalResources.ProviderAccountIDs))
		for _, s := range r.ExternalResources.ProviderAccountIDs {
			if s == "" {
				continue
			}
			parsed, err := id.Parse(s)
			if err != nil {
				return patch, fmt.Errorf("invalid externalResources.providerAccountIds: %w", err)
			}
			accountIDs = append(accountIDs, parsed)
		}
		patch.ExternalResources = &crypto.ExternalResourcePolicy{
			Strategy:                 crypto.ResourceStrategyPolicy(r.ExternalResources.Strategy),
			ProviderAccountIDs:       accountIDs,
			ProviderFailureThreshold: r.ExternalResources.ProviderFailureThreshold,
			ProviderOpenDuration:     time.Duration(r.ExternalResources.ProviderOpenSeconds) * time.Second,
		}
	}
	if r.RetryDelaySeconds != nil {
		value := time.Duration(*r.RetryDelaySeconds) * time.Second
		patch.RetryDelay = &value
	}
	if r.RPCOpenSeconds != nil {
		value := time.Duration(*r.RPCOpenSeconds) * time.Second
		patch.RPCOpenDuration = &value
	}
	return patch, nil
}
