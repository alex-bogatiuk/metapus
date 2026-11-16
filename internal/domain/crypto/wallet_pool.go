package crypto

import (
	"context"
	"fmt"
	"strings"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

const (
	WalletPoolStateHealthy  = "healthy"
	WalletPoolStateLow      = "low"
	WalletPoolStateDepleted = "depleted"
	WalletPoolStatePaused   = "paused"

	WalletPoolErrorVaultUnavailable        = "VAULT_UNAVAILABLE"
	WalletPoolErrorVaultInvalidResponse    = "VAULT_INVALID_RESPONSE"
	WalletPoolErrorChainUnavailable        = "CHAIN_UNAVAILABLE"
	WalletPoolErrorDriverUnavailable       = "NETWORK_DRIVER_UNAVAILABLE"
	WalletPoolErrorProvisioningUnsupported = "WALLET_PROVISIONING_UNSUPPORTED"
	WalletPoolErrorPolicyInvalid           = "WALLET_POOL_POLICY_INVALID"
	WalletPoolErrorActivationHeld          = "ACTIVATION_HELD"
)

// WalletPoolActivationPolicy is present only for families whose receiving
// addresses must be materialized on-chain before allocation.
type WalletPoolActivationPolicy struct {
	MaxConcurrent      int             `json:"maxConcurrent"`
	ReserveMinor       types.RawAmount `json:"reserveMinor"`
	MinHotBalanceMinor types.RawAmount `json:"minHotBalanceMinor"`
	DailyBudgetMinor   types.RawAmount `json:"dailyBudgetMinor"`
}

// WalletPoolPolicy controls warm-pool capacity for one blockchain network.
// All monetary values are integer minor units of the network native token.
type WalletPoolPolicy struct {
	NetworkID                          id.ID                       `json:"networkId"`
	NetworkCode                        string                      `json:"networkCode"`
	NetworkName                        string                      `json:"networkName"`
	ChainFamily                        ChainFamily                 `json:"chainFamily"`
	NativeTokenSymbol                  string                      `json:"nativeTokenSymbol"`
	NativeDecimals                     int                         `json:"nativeDecimals"`
	Enabled                            bool                        `json:"enabled"`
	LowWatermark                       int                         `json:"lowWatermark"`
	TargetReady                        int                         `json:"targetReady"`
	MaxTotalWallets                    int                         `json:"maxTotalWallets"`
	MaxProvisionBatch                  int                         `json:"maxProvisionBatch"`
	MaxActiveInvoiceLeasesPerMerchant  int                         `json:"maxActiveInvoiceLeasesPerMerchant"`
	MaxPersistentAddressesPerMerchant  int                         `json:"maxPersistentAddressesPerMerchant"`
	MaxLeaseStartsPerMinutePerMerchant int                         `json:"maxLeaseStartsPerMinutePerMerchant"`
	MinGlobalReadyReserve              int                         `json:"minGlobalReadyReserve"`
	Activation                         *WalletPoolActivationPolicy `json:"activation,omitempty"`
	RetryAfterSeconds                  int                         `json:"retryAfterSeconds"`
	AddressReuseCooldownSeconds        int                         `json:"addressReuseCooldownSeconds"`
	MaxWatcherLagSeconds               int                         `json:"maxWatcherLagSeconds"`
	LeaseBlockedReason                 string                      `json:"leaseBlockedReason,omitempty"`
	LeaseBlockedAt                     *time.Time                  `json:"leaseBlockedAt,omitempty"`
	DerivationNamespace                string                      `json:"derivationNamespace"`
	NeedsReview                        bool                        `json:"needsReview"`
	Version                            int                         `json:"version"`
	UpdatedAt                          time.Time                   `json:"updatedAt"`
	UpdatedBy                          string                      `json:"updatedBy"`
	LastReconciledAt                   *time.Time                  `json:"lastReconciledAt,omitempty"`
	LastErrorCode                      string                      `json:"lastErrorCode,omitempty"`
	LastErrorMessage                   string                      `json:"lastErrorMessage,omitempty"`
}

// Validate rejects configurations that are inconsistent with the installed
// family adapter or could overrun capacity and spend limits.
func (p WalletPoolPolicy) Validate(capabilities ChainFamilyCapabilities) error {
	if err := capabilities.Validate(); err != nil {
		return apperror.NewValidation("invalid wallet pool capabilities").WithCause(err)
	}
	switch {
	case !p.ChainFamily.Valid():
		return apperror.NewValidation("valid chainFamily is required")
	case capabilities.Family != p.ChainFamily:
		return apperror.NewValidation("wallet pool capabilities do not match chainFamily")
	case p.Enabled && !capabilities.WalletProvisioning.Supported():
		return apperror.NewValidation("automatic wallet provisioning is not supported for this network")
	case p.LowWatermark < 0:
		return apperror.NewValidation("lowWatermark must be non-negative")
	case p.LowWatermark >= p.TargetReady:
		return apperror.NewValidation("lowWatermark must be less than targetReady")
	case p.TargetReady > p.MaxTotalWallets:
		return apperror.NewValidation("targetReady must not exceed maxTotalWallets")
	case p.MaxProvisionBatch <= 0:
		return apperror.NewValidation("maxProvisionBatch must be positive")
	case p.MaxActiveInvoiceLeasesPerMerchant <= 0:
		return apperror.NewValidation("maxActiveInvoiceLeasesPerMerchant must be positive")
	case p.MaxPersistentAddressesPerMerchant <= 0:
		return apperror.NewValidation("maxPersistentAddressesPerMerchant must be positive")
	case p.MaxLeaseStartsPerMinutePerMerchant <= 0:
		return apperror.NewValidation("maxLeaseStartsPerMinutePerMerchant must be positive")
	case p.MinGlobalReadyReserve < 0:
		return apperror.NewValidation("minGlobalReadyReserve must be non-negative")
	case p.MinGlobalReadyReserve > p.LowWatermark:
		return apperror.NewValidation("minGlobalReadyReserve must not exceed lowWatermark")
	case p.RetryAfterSeconds <= 0:
		return apperror.NewValidation("retryAfterSeconds must be positive")
	case p.AddressReuseCooldownSeconds < 0:
		return apperror.NewValidation("addressReuseCooldownSeconds must be non-negative")
	case p.MaxWatcherLagSeconds < 0:
		return apperror.NewValidation("maxWatcherLagSeconds must be non-negative")
	case p.AddressReuseCooldownSeconds > 0 && p.MaxWatcherLagSeconds <= 0:
		return apperror.NewValidation("maxWatcherLagSeconds must be positive when address reuse cooldown is enabled")
	case p.AddressReuseCooldownSeconds > 0 && capabilities.MinWatcherLagSeconds > 0 && p.MaxWatcherLagSeconds < capabilities.MinWatcherLagSeconds:
		return apperror.NewValidation(fmt.Sprintf("maxWatcherLagSeconds must be at least %d when cooldown is enabled", capabilities.MinWatcherLagSeconds))
	case p.LeaseBlockedReason != "" && p.LeaseBlockedReason != LeaseBlockedReasonWatcherLag:
		return apperror.NewValidation("leaseBlockedReason is invalid")
	case p.Version <= 0:
		return apperror.NewValidation("version must be positive")
	case p.Enabled && p.NeedsReview:
		return apperror.NewValidation("wallet pool cannot be enabled while derivation generation needs review")
	case p.Enabled && strings.TrimSpace(p.DerivationNamespace) == "":
		return apperror.NewValidation("derivationNamespace is required when wallet pool is enabled")
	case capabilities.WalletProvisioning.RequiresActivation() && p.Enabled && p.Activation == nil:
		return apperror.NewValidation("activation policy is required for this chain family")
	case !capabilities.WalletProvisioning.RequiresActivation() && p.Activation != nil:
		return apperror.NewValidation("activation policy is not supported for this chain family")
	}
	if p.Activation != nil {
		return p.Activation.Validate()
	}
	return nil
}

func (p WalletPoolActivationPolicy) Validate() error {
	if p.MaxConcurrent <= 0 {
		return apperror.NewValidation("activation.maxConcurrent must be positive")
	}
	return nil
}

type WalletPoolActivationPolicyPatch struct {
	MaxConcurrent      *int
	ReserveMinor       *types.RawAmount
	MinHotBalanceMinor *types.RawAmount
	DailyBudgetMinor   *types.RawAmount
}

// WalletPoolPolicyPatch is the transport-independent update command. Pointer
// fields preserve PATCH semantics without coupling the domain to HTTP DTOs.
type WalletPoolPolicyPatch struct {
	Enabled                            *bool
	LowWatermark                       *int
	TargetReady                        *int
	MaxTotalWallets                    *int
	MaxProvisionBatch                  *int
	MaxActiveInvoiceLeasesPerMerchant  *int
	MaxPersistentAddressesPerMerchant  *int
	MaxLeaseStartsPerMinutePerMerchant *int
	MinGlobalReadyReserve              *int
	Activation                         *WalletPoolActivationPolicyPatch
	RetryAfterSeconds                  *int
	AddressReuseCooldownSeconds        *int
	MaxWatcherLagSeconds               *int
	Version                            int
}

// MerchantWalletAllocationOverride contains optional exceptions to one
// network's merchant defaults. Nil values inherit the network policy.
type MerchantWalletAllocationOverride struct {
	MerchantID              id.ID
	NetworkID               id.ID
	MaxActiveInvoiceLeases  *int
	MaxPersistentAddresses  *int
	MaxLeaseStartsPerMinute *int
	Version                 int
	UpdatedAt               time.Time
	UpdatedBy               string
}

func (o MerchantWalletAllocationOverride) Validate() error {
	switch {
	case o.MaxActiveInvoiceLeases != nil && *o.MaxActiveInvoiceLeases <= 0:
		return apperror.NewValidation("maxActiveInvoiceLeases must be positive")
	case o.MaxPersistentAddresses != nil && *o.MaxPersistentAddresses <= 0:
		return apperror.NewValidation("maxPersistentAddresses must be positive")
	case o.MaxLeaseStartsPerMinute != nil && *o.MaxLeaseStartsPerMinute <= 0:
		return apperror.NewValidation("maxLeaseStartsPerMinute must be positive")
	case o.Version < 0:
		return apperror.NewValidation("version must be non-negative")
	}
	return nil
}

// MerchantWalletAllocationLimits is the admin projection containing both
// nullable overrides and the effective values enforced by admission.
type MerchantWalletAllocationLimits struct {
	MerchantWalletAllocationOverride
	EffectiveMaxActiveInvoiceLeases  int
	EffectiveMaxPersistentAddresses  int
	EffectiveMaxLeaseStartsPerMinute int
}

func ResolveMerchantWalletAllocationLimits(
	policy WalletPoolPolicy,
	override MerchantWalletAllocationOverride,
) MerchantWalletAllocationLimits {
	result := MerchantWalletAllocationLimits{
		MerchantWalletAllocationOverride: override,
		EffectiveMaxActiveInvoiceLeases:  policy.MaxActiveInvoiceLeasesPerMerchant,
		EffectiveMaxPersistentAddresses:  policy.MaxPersistentAddressesPerMerchant,
		EffectiveMaxLeaseStartsPerMinute: policy.MaxLeaseStartsPerMinutePerMerchant,
	}
	if override.MaxActiveInvoiceLeases != nil {
		result.EffectiveMaxActiveInvoiceLeases = *override.MaxActiveInvoiceLeases
	}
	if override.MaxPersistentAddresses != nil {
		result.EffectiveMaxPersistentAddresses = *override.MaxPersistentAddresses
	}
	if override.MaxLeaseStartsPerMinute != nil {
		result.EffectiveMaxLeaseStartsPerMinute = *override.MaxLeaseStartsPerMinute
	}
	return result
}

// ApplyPatch returns a copy and leaves the current aggregate untouched.
func (p WalletPoolPolicy) ApplyPatch(patch WalletPoolPolicyPatch) WalletPoolPolicy {
	p.Version = patch.Version
	if patch.Enabled != nil {
		p.Enabled = *patch.Enabled
	}
	if patch.LowWatermark != nil {
		p.LowWatermark = *patch.LowWatermark
	}
	if patch.TargetReady != nil {
		p.TargetReady = *patch.TargetReady
	}
	if patch.MaxTotalWallets != nil {
		p.MaxTotalWallets = *patch.MaxTotalWallets
	}
	if patch.MaxProvisionBatch != nil {
		p.MaxProvisionBatch = *patch.MaxProvisionBatch
	}
	if patch.MaxActiveInvoiceLeasesPerMerchant != nil {
		p.MaxActiveInvoiceLeasesPerMerchant = *patch.MaxActiveInvoiceLeasesPerMerchant
	}
	if patch.MaxPersistentAddressesPerMerchant != nil {
		p.MaxPersistentAddressesPerMerchant = *patch.MaxPersistentAddressesPerMerchant
	}
	if patch.MaxLeaseStartsPerMinutePerMerchant != nil {
		p.MaxLeaseStartsPerMinutePerMerchant = *patch.MaxLeaseStartsPerMinutePerMerchant
	}
	if patch.MinGlobalReadyReserve != nil {
		p.MinGlobalReadyReserve = *patch.MinGlobalReadyReserve
	}
	if patch.Activation != nil {
		activation := WalletPoolActivationPolicy{}
		if p.Activation != nil {
			activation = *p.Activation
		}
		if patch.Activation.MaxConcurrent != nil {
			activation.MaxConcurrent = *patch.Activation.MaxConcurrent
		}
		if patch.Activation.ReserveMinor != nil {
			activation.ReserveMinor = *patch.Activation.ReserveMinor
		}
		if patch.Activation.MinHotBalanceMinor != nil {
			activation.MinHotBalanceMinor = *patch.Activation.MinHotBalanceMinor
		}
		if patch.Activation.DailyBudgetMinor != nil {
			activation.DailyBudgetMinor = *patch.Activation.DailyBudgetMinor
		}
		p.Activation = &activation
	}
	if patch.RetryAfterSeconds != nil {
		p.RetryAfterSeconds = *patch.RetryAfterSeconds
	}
	if patch.AddressReuseCooldownSeconds != nil {
		p.AddressReuseCooldownSeconds = *patch.AddressReuseCooldownSeconds
	}
	if patch.MaxWatcherLagSeconds != nil {
		p.MaxWatcherLagSeconds = *patch.MaxWatcherLagSeconds
	}
	return p
}

// WalletPoolStats is an address-free operational projection for one network.
type WalletPoolStats struct {
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

// WalletPoolState combines policy and aggregate status for the Admin API.
type WalletPoolState struct {
	Policy       WalletPoolPolicy        `json:"policy"`
	Capabilities ChainFamilyCapabilities `json:"capabilities"`
	Stats        WalletPoolStats         `json:"stats"`
	State        string                  `json:"state"`
}

func ComputeWalletPoolState(policy WalletPoolPolicy, stats WalletPoolStats) string {
	if !policy.Enabled || policy.NeedsReview {
		return WalletPoolStatePaused
	}
	if stats.Ready == 0 {
		return WalletPoolStateDepleted
	}
	if stats.Ready <= policy.LowWatermark {
		return WalletPoolStateLow
	}
	return WalletPoolStateHealthy
}

type WalletProvisioningJobStatus string

const (
	WalletProvisioningPending    WalletProvisioningJobStatus = "pending"
	WalletProvisioningProcessing WalletProvisioningJobStatus = "processing"
	WalletProvisioningRetryWait  WalletProvisioningJobStatus = "retry_wait"
	WalletProvisioningCompleted  WalletProvisioningJobStatus = "completed"
	WalletProvisioningHeld       WalletProvisioningJobStatus = "held"
)

// WalletProvisioningJob is a durable, resumable Vault derivation request.
type WalletProvisioningJob struct {
	ID             id.ID
	NetworkID      id.ID
	NetworkCode    string
	DerivationPath string
	Status         WalletProvisioningJobStatus
	Attempts       int
	NextAttemptAt  time.Time
	WalletID       *id.ID
	ClaimedBy      string
	ClaimToken     id.ID
}

// WalletPendingReconciliation identifies a provisioned wallet that cannot
// enter activation until its initial chain-vs-ledger check succeeds.
type WalletPendingReconciliation struct {
	WalletID  id.ID
	NetworkID id.ID
}

// WalletPoolPlanResult describes an atomic watermark reconciliation.
type WalletPoolPlanResult struct {
	NetworkID id.ID
	Ready     int
	InFlight  int
	Total     int
	Created   int
}

// WalletPoolOperationalStatus is the safe, stable projection persisted for
// administrators. Raw dependency errors remain in structured server logs.
type WalletPoolOperationalStatus struct {
	ErrorCode     string
	PublicMessage string
}

func WalletPoolHealthyStatus() WalletPoolOperationalStatus {
	return WalletPoolOperationalStatus{}
}

// WalletPoolAdminRepository is the persistence port used by admin use cases.
type WalletPoolAdminRepository interface {
	ListStates(ctx context.Context) ([]WalletPoolState, error)
	GetState(ctx context.Context, networkID id.ID) (WalletPoolState, error)
	UpdatePolicy(ctx context.Context, policy WalletPoolPolicy, updatedBy string) (WalletPoolState, error)
	HasConfirmedNativeFunding(ctx context.Context, networkID id.ID) (bool, error)
	PlanNetworkRefill(ctx context.Context, networkID id.ID) (WalletPoolPlanResult, error)
	GetMerchantAllocationOverride(ctx context.Context, merchantID, networkID id.ID) (MerchantWalletAllocationOverride, bool, error)
	UpsertMerchantAllocationOverride(ctx context.Context, override MerchantWalletAllocationOverride, updatedBy string) (MerchantWalletAllocationOverride, error)
}

// WalletPoolControllerRepository is the narrow persistence port used by the
// asynchronous refill controller.
type WalletPoolControllerRepository interface {
	ListStates(ctx context.Context) ([]WalletPoolState, error)
	GetState(ctx context.Context, networkID id.ID) (WalletPoolState, error)
	ListRefillCandidates(ctx context.Context) ([]id.ID, error)
	PlanNetworkRefill(ctx context.Context, networkID id.ID) (WalletPoolPlanResult, error)
	ClaimProvisioningJobs(ctx context.Context, workerID string, limit int, lease time.Duration) ([]WalletProvisioningJob, error)
	CompleteProvisioningJob(ctx context.Context, job WalletProvisioningJob, derivation WalletDerivation, mode WalletProvisioningMode) (id.ID, error)
	RetryProvisioningJob(ctx context.Context, job WalletProvisioningJob, err error, nextAttemptAt time.Time, held bool) error
	ListPendingWalletReconciliations(ctx context.Context, limit int) ([]WalletPendingReconciliation, error)
	SetOperationalStatus(ctx context.Context, networkID id.ID, status WalletPoolOperationalStatus) error
	RecordStateTransition(ctx context.Context, state WalletPoolState) error
}

// WalletPoolActivationStatusRepository is the minimal port used by activation
// execution to publish safe pool health.
type WalletPoolActivationStatusRepository interface {
	GetState(ctx context.Context, networkID id.ID) (WalletPoolState, error)
	SetOperationalStatus(ctx context.Context, networkID id.ID, status WalletPoolOperationalStatus) error
}

type WalletPoolWorkerRepository interface {
	WalletPoolControllerRepository
	WalletPoolActivationStatusRepository
}

// ValidateWalletDerivation confirms that Vault returned the immutable job path.
func ValidateWalletDerivation(job WalletProvisioningJob, result WalletDerivation) error {
	if strings.TrimSpace(result.Address) == "" {
		return fmt.Errorf("vault derivation address is empty")
	}
	if strings.TrimSpace(result.VaultKeyID) == "" {
		return fmt.Errorf("vault derivation key id is empty")
	}
	if result.DerivationPath != job.DerivationPath {
		return fmt.Errorf("vault derivation path mismatch: expected %q, got %q", job.DerivationPath, result.DerivationPath)
	}
	return nil
}
