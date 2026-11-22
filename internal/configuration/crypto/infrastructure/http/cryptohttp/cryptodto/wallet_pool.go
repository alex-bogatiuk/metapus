package cryptodto

import (
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/types"
)

type UpdateWalletPoolPolicyRequest struct {
	Enabled                            *bool                                    `json:"enabled"`
	LowWatermark                       *int                                     `json:"lowWatermark"`
	TargetReady                        *int                                     `json:"targetReady"`
	MaxTotalWallets                    *int                                     `json:"maxTotalWallets"`
	MaxProvisionBatch                  *int                                     `json:"maxProvisionBatch"`
	MaxActiveInvoiceLeasesPerMerchant  *int                                     `json:"maxActiveInvoiceLeasesPerMerchant"`
	MaxPersistentAddressesPerMerchant  *int                                     `json:"maxPersistentAddressesPerMerchant"`
	MaxLeaseStartsPerMinutePerMerchant *int                                     `json:"maxLeaseStartsPerMinutePerMerchant"`
	MinGlobalReadyReserve              *int                                     `json:"minGlobalReadyReserve"`
	Activation                         *UpdateWalletPoolActivationPolicyRequest `json:"activation"`
	RetryAfterSeconds                  *int                                     `json:"retryAfterSeconds"`
	AddressReuseCooldownSeconds        *int                                     `json:"addressReuseCooldownSeconds"`
	MaxWatcherLagSeconds               *int                                     `json:"maxWatcherLagSeconds"`
	Version                            int                                      `json:"version" binding:"required"`
}

func (r UpdateWalletPoolPolicyRequest) ToDomain() crypto.WalletPoolPolicyPatch {
	patch := crypto.WalletPoolPolicyPatch{
		Enabled:                            r.Enabled,
		LowWatermark:                       r.LowWatermark,
		TargetReady:                        r.TargetReady,
		MaxTotalWallets:                    r.MaxTotalWallets,
		MaxProvisionBatch:                  r.MaxProvisionBatch,
		MaxActiveInvoiceLeasesPerMerchant:  r.MaxActiveInvoiceLeasesPerMerchant,
		MaxPersistentAddressesPerMerchant:  r.MaxPersistentAddressesPerMerchant,
		MaxLeaseStartsPerMinutePerMerchant: r.MaxLeaseStartsPerMinutePerMerchant,
		MinGlobalReadyReserve:              r.MinGlobalReadyReserve,
		RetryAfterSeconds:                  r.RetryAfterSeconds,
		AddressReuseCooldownSeconds:        r.AddressReuseCooldownSeconds,
		MaxWatcherLagSeconds:               r.MaxWatcherLagSeconds,
		Version:                            r.Version,
	}
	if r.Activation != nil {
		patch.Activation = &crypto.WalletPoolActivationPolicyPatch{
			MaxConcurrent:      r.Activation.MaxConcurrent,
			ReserveMinor:       r.Activation.ReserveMinor,
			MinHotBalanceMinor: r.Activation.MinHotBalanceMinor,
			DailyBudgetMinor:   r.Activation.DailyBudgetMinor,
		}
	}
	return patch
}

type UpdateWalletPoolActivationPolicyRequest struct {
	MaxConcurrent      *int             `json:"maxConcurrent"`
	ReserveMinor       *types.RawAmount `json:"reserveMinor"`
	MinHotBalanceMinor *types.RawAmount `json:"minHotBalanceMinor"`
	DailyBudgetMinor   *types.RawAmount `json:"dailyBudgetMinor"`
}

type WalletPoolActivationPolicyResponse struct {
	MaxConcurrent      int             `json:"maxConcurrent"`
	ReserveMinor       types.RawAmount `json:"reserveMinor"`
	MinHotBalanceMinor types.RawAmount `json:"minHotBalanceMinor"`
	DailyBudgetMinor   types.RawAmount `json:"dailyBudgetMinor"`
}

type WalletPoolPolicyResponse struct {
	NetworkID                          string                              `json:"networkId"`
	NetworkCode                        string                              `json:"networkCode"`
	NetworkName                        string                              `json:"networkName"`
	ChainFamily                        crypto.ChainFamily                  `json:"chainFamily"`
	NativeTokenSymbol                  string                              `json:"nativeTokenSymbol"`
	NativeDecimals                     int                                 `json:"nativeDecimals"`
	Enabled                            bool                                `json:"enabled"`
	LowWatermark                       int                                 `json:"lowWatermark"`
	TargetReady                        int                                 `json:"targetReady"`
	MaxTotalWallets                    int                                 `json:"maxTotalWallets"`
	MaxProvisionBatch                  int                                 `json:"maxProvisionBatch"`
	MaxActiveInvoiceLeasesPerMerchant  int                                 `json:"maxActiveInvoiceLeasesPerMerchant"`
	MaxPersistentAddressesPerMerchant  int                                 `json:"maxPersistentAddressesPerMerchant"`
	MaxLeaseStartsPerMinutePerMerchant int                                 `json:"maxLeaseStartsPerMinutePerMerchant"`
	MinGlobalReadyReserve              int                                 `json:"minGlobalReadyReserve"`
	Activation                         *WalletPoolActivationPolicyResponse `json:"activation"`
	RetryAfterSeconds                  int                                 `json:"retryAfterSeconds"`
	AddressReuseCooldownSeconds        int                                 `json:"addressReuseCooldownSeconds"`
	MaxWatcherLagSeconds               int                                 `json:"maxWatcherLagSeconds"`
	LeaseBlockedReason                 string                              `json:"leaseBlockedReason,omitempty"`
	LeaseBlockedAt                     *time.Time                          `json:"leaseBlockedAt,omitempty"`
	DerivationNamespace                string                              `json:"derivationNamespace"`
	NeedsReview                        bool                                `json:"needsReview"`
	Version                            int                                 `json:"version"`
	UpdatedAt                          time.Time                           `json:"updatedAt"`
	UpdatedBy                          string                              `json:"updatedBy"`
	LastReconciledAt                   *time.Time                          `json:"lastReconciledAt,omitempty"`
	LastErrorCode                      string                              `json:"lastErrorCode,omitempty"`
	LastErrorMessage                   string                              `json:"lastErrorMessage,omitempty"`
}

type WalletPoolStatsResponse struct {
	Ready            int `json:"ready"`
	Cooldown         int `json:"cooldown"`
	Leased           int `json:"leased"`
	Assigned         int `json:"assigned"`
	Provisioning     int `json:"provisioning"`
	Activating       int `json:"activating"`
	SweepPending     int `json:"sweepPending"`
	Held             int `json:"held"`
	Failed           int `json:"failed"`
	Total            int `json:"total"`
	InboxUnmatched   int `json:"inboxUnmatched"`
	InboxNeedsReview int `json:"inboxNeedsReview"`
	InboxPoison      int `json:"inboxPoison"`
}

type WalletPoolStateResponse struct {
	Policy       WalletPoolPolicyResponse       `json:"policy"`
	Capabilities WalletPoolCapabilitiesResponse `json:"capabilities"`
	Stats        WalletPoolStatsResponse        `json:"stats"`
	State        string                         `json:"state"`
}

type WalletPoolCapabilitiesResponse struct {
	Family             crypto.ChainFamily            `json:"family"`
	WalletProvisioning crypto.WalletProvisioningMode `json:"walletProvisioning"`
}

type WalletPoolListResponse struct {
	Items []WalletPoolStateResponse `json:"items"`
}

type WalletPoolPlanResultResponse struct {
	NetworkID string `json:"networkId"`
	Ready     int    `json:"ready"`
	InFlight  int    `json:"inFlight"`
	Total     int    `json:"total"`
	Created   int    `json:"created"`
}

type WalletPoolReconcileResponse struct {
	Accepted bool                           `json:"accepted"`
	Results  []WalletPoolPlanResultResponse `json:"results"`
	State    WalletPoolStateResponse        `json:"state"`
}

type UpdateMerchantWalletAllocationLimitsRequest struct {
	MaxActiveInvoiceLeases  *int `json:"maxActiveInvoiceLeases"`
	MaxPersistentAddresses  *int `json:"maxPersistentAddresses"`
	MaxLeaseStartsPerMinute *int `json:"maxLeaseStartsPerMinute"`
	Version                 int  `json:"version"`
}

func (r UpdateMerchantWalletAllocationLimitsRequest) ToDomain() crypto.MerchantWalletAllocationOverride {
	return crypto.MerchantWalletAllocationOverride{
		MaxActiveInvoiceLeases:  r.MaxActiveInvoiceLeases,
		MaxPersistentAddresses:  r.MaxPersistentAddresses,
		MaxLeaseStartsPerMinute: r.MaxLeaseStartsPerMinute,
		Version:                 r.Version,
	}
}

type MerchantWalletAllocationLimitsResponse struct {
	MerchantID                       string     `json:"merchantId"`
	NetworkID                        string     `json:"networkId"`
	MaxActiveInvoiceLeases           *int       `json:"maxActiveInvoiceLeases"`
	MaxPersistentAddresses           *int       `json:"maxPersistentAddresses"`
	MaxLeaseStartsPerMinute          *int       `json:"maxLeaseStartsPerMinute"`
	EffectiveMaxActiveInvoiceLeases  int        `json:"effectiveMaxActiveInvoiceLeases"`
	EffectiveMaxPersistentAddresses  int        `json:"effectiveMaxPersistentAddresses"`
	EffectiveMaxLeaseStartsPerMinute int        `json:"effectiveMaxLeaseStartsPerMinute"`
	Version                          int        `json:"version"`
	UpdatedAt                        *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy                        string     `json:"updatedBy,omitempty"`
}

func NewMerchantWalletAllocationLimitsResponse(
	limits crypto.MerchantWalletAllocationLimits,
) MerchantWalletAllocationLimitsResponse {
	var updatedAt *time.Time
	if limits.Version > 0 {
		value := limits.UpdatedAt
		updatedAt = &value
	}
	return MerchantWalletAllocationLimitsResponse{
		MerchantID:                       limits.MerchantID.String(),
		NetworkID:                        limits.NetworkID.String(),
		MaxActiveInvoiceLeases:           limits.MaxActiveInvoiceLeases,
		MaxPersistentAddresses:           limits.MaxPersistentAddresses,
		MaxLeaseStartsPerMinute:          limits.MaxLeaseStartsPerMinute,
		EffectiveMaxActiveInvoiceLeases:  limits.EffectiveMaxActiveInvoiceLeases,
		EffectiveMaxPersistentAddresses:  limits.EffectiveMaxPersistentAddresses,
		EffectiveMaxLeaseStartsPerMinute: limits.EffectiveMaxLeaseStartsPerMinute,
		Version:                          limits.Version,
		UpdatedAt:                        updatedAt,
		UpdatedBy:                        limits.UpdatedBy,
	}
}

func NewWalletPoolListResponse(states []crypto.WalletPoolState) WalletPoolListResponse {
	items := make([]WalletPoolStateResponse, 0, len(states))
	for _, state := range states {
		items = append(items, NewWalletPoolStateResponse(state))
	}
	return WalletPoolListResponse{Items: items}
}

func NewWalletPoolStateResponse(state crypto.WalletPoolState) WalletPoolStateResponse {
	policy := state.Policy
	stats := state.Stats
	var activation *WalletPoolActivationPolicyResponse
	if policy.Activation != nil {
		activation = &WalletPoolActivationPolicyResponse{
			MaxConcurrent:      policy.Activation.MaxConcurrent,
			ReserveMinor:       policy.Activation.ReserveMinor,
			MinHotBalanceMinor: policy.Activation.MinHotBalanceMinor,
			DailyBudgetMinor:   policy.Activation.DailyBudgetMinor,
		}
	}
	return WalletPoolStateResponse{
		Policy: WalletPoolPolicyResponse{
			NetworkID:                          policy.NetworkID.String(),
			NetworkCode:                        policy.NetworkCode,
			NetworkName:                        policy.NetworkName,
			ChainFamily:                        policy.ChainFamily,
			NativeTokenSymbol:                  policy.NativeTokenSymbol,
			NativeDecimals:                     policy.NativeDecimals,
			Enabled:                            policy.Enabled,
			LowWatermark:                       policy.LowWatermark,
			TargetReady:                        policy.TargetReady,
			MaxTotalWallets:                    policy.MaxTotalWallets,
			MaxProvisionBatch:                  policy.MaxProvisionBatch,
			MaxActiveInvoiceLeasesPerMerchant:  policy.MaxActiveInvoiceLeasesPerMerchant,
			MaxPersistentAddressesPerMerchant:  policy.MaxPersistentAddressesPerMerchant,
			MaxLeaseStartsPerMinutePerMerchant: policy.MaxLeaseStartsPerMinutePerMerchant,
			MinGlobalReadyReserve:              policy.MinGlobalReadyReserve,
			Activation:                         activation,
			RetryAfterSeconds:                  policy.RetryAfterSeconds,
			AddressReuseCooldownSeconds:        policy.AddressReuseCooldownSeconds,
			MaxWatcherLagSeconds:               policy.MaxWatcherLagSeconds,
			LeaseBlockedReason:                 policy.LeaseBlockedReason,
			LeaseBlockedAt:                     policy.LeaseBlockedAt,
			DerivationNamespace:                policy.DerivationNamespace,
			NeedsReview:                        policy.NeedsReview,
			Version:                            policy.Version,
			UpdatedAt:                          policy.UpdatedAt,
			UpdatedBy:                          policy.UpdatedBy,
			LastReconciledAt:                   policy.LastReconciledAt,
			LastErrorCode:                      policy.LastErrorCode,
			LastErrorMessage:                   policy.LastErrorMessage,
		},
		Capabilities: WalletPoolCapabilitiesResponse{
			Family:             state.Capabilities.Family,
			WalletProvisioning: state.Capabilities.WalletProvisioning,
		},
		Stats: WalletPoolStatsResponse{
			Ready:            stats.Ready,
			Cooldown:         stats.Cooldown,
			Leased:           stats.Leased,
			Assigned:         stats.Assigned,
			Provisioning:     stats.Provisioning,
			Activating:       stats.Activating,
			SweepPending:     stats.SweepPending,
			Held:             stats.Held,
			Failed:           stats.Failed,
			Total:            stats.Total,
			InboxUnmatched:   stats.InboxUnmatched,
			InboxNeedsReview: stats.InboxNeedsReview,
			InboxPoison:      stats.InboxPoison,
		},
		State: state.State,
	}
}

func NewWalletPoolReconcileResponse(
	results []crypto.WalletPoolPlanResult,
	state crypto.WalletPoolState,
) WalletPoolReconcileResponse {
	items := make([]WalletPoolPlanResultResponse, 0, len(results))
	for _, result := range results {
		items = append(items, WalletPoolPlanResultResponse{
			NetworkID: result.NetworkID.String(),
			Ready:     result.Ready,
			InFlight:  result.InFlight,
			Total:     result.Total,
			Created:   result.Created,
		})
	}
	return WalletPoolReconcileResponse{
		Accepted: true,
		Results:  items,
		State:    NewWalletPoolStateResponse(state),
	}
}
