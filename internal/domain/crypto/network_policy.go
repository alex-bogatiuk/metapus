package crypto

import (
	"context"
	"errors"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type ResourceStrategyPolicy string

const (
	ResourceRentalRequired ResourceStrategyPolicy = "rental_required"
	ResourcePreferRental   ResourceStrategyPolicy = "prefer_rental"
	ResourceBurnOnly       ResourceStrategyPolicy = "burn_only"
)

func (p ResourceStrategyPolicy) Valid() bool {
	return p == ResourceRentalRequired || p == ResourcePreferRental || p == ResourceBurnOnly
}

type FeeFundingMode string

const (
	FeeFundingSourceOnly FeeFundingMode = "source_only"
	FeeFundingJIT        FeeFundingMode = "jit"
)

func (m FeeFundingMode) Valid() bool { return m == FeeFundingSourceOnly || m == FeeFundingJIT }

type FeeFundingPolicy struct {
	Mode                    FeeFundingMode     `json:"mode"`
	DailyBudget             types.CryptoAmount `json:"dailyBudgetMinor"`
	MinFeeWalletBalance     types.CryptoAmount `json:"minFeeWalletBalanceMinor"`
	MinSourceWalletResidual types.CryptoAmount `json:"minSourceWalletResidualMinor"`
	MaxFundingAmount        types.CryptoAmount `json:"maxFundingAmountMinor"`
}

type ExternalResourcePolicy struct {
	Strategy                 ResourceStrategyPolicy `json:"strategy"`
	ProviderAccountIDs       []id.ID                `json:"providerAccountIds"`
	ProviderFailureThreshold int                    `json:"providerFailureThreshold"`
	ProviderOpenDuration     time.Duration          `json:"providerOpenDuration"`
}

func (p *ExternalResourcePolicy) Validate() error {
	if p == nil || !p.Strategy.Valid() || p.ProviderFailureThreshold <= 0 || p.ProviderOpenDuration <= 0 {
		return errors.New("external resource policy requires a strategy and positive circuit values")
	}
	seenAccounts := make(map[id.ID]struct{}, len(p.ProviderAccountIDs))
	for _, accountID := range p.ProviderAccountIDs {
		if id.IsNil(accountID) {
			return errors.New("resource provider account id cannot be nil")
		}
		if _, duplicate := seenAccounts[accountID]; duplicate {
			return fmt.Errorf("duplicate resource provider account %s", accountID)
		}
		seenAccounts[accountID] = struct{}{}
	}
	if p.Strategy != ResourceBurnOnly && len(p.ProviderAccountIDs) == 0 {
		return errors.New("resource rental strategy requires at least one provider account")
	}
	return nil
}

type NetworkPolicy struct {
	NetworkID             id.ID                   `json:"networkId"`
	Enabled               bool                    `json:"enabled"`
	PerTransactionMaxFee  types.CryptoAmount      `json:"perTransactionMaxFeeMinor"`
	DailyNetworkFeeBudget types.CryptoAmount      `json:"dailyNetworkFeeBudgetMinor"`
	FeeFunding            FeeFundingPolicy        `json:"feeFunding"`
	ExternalResources     *ExternalResourcePolicy `json:"externalResources,omitempty"`
	RetryDelay            time.Duration           `json:"retryDelay"`
	RPCFailureThreshold   int                     `json:"rpcFailureThreshold"`
	RPCOpenDuration       time.Duration           `json:"rpcOpenDuration"`
	Version               int                     `json:"version"`
	UpdatedBy             string                  `json:"updatedBy"`
	CreatedAt             time.Time               `json:"createdAt"`
	UpdatedAt             time.Time               `json:"updatedAt"`
	OperationalHold       bool                    `json:"operationalHold"`
	OperationalHoldReason string                  `json:"operationalHoldReason,omitempty"`
}

func (p NetworkPolicy) Validate() error {
	if id.IsNil(p.NetworkID) || p.Version <= 0 {
		return errors.New("network and positive version are required")
	}
	if p.RetryDelay <= 0 || p.RPCFailureThreshold <= 0 || p.RPCOpenDuration <= 0 {
		return errors.New("network policy retry and RPC circuit values must be positive")
	}
	if p.Enabled {
		if !p.PerTransactionMaxFee.IsPositive() || !p.DailyNetworkFeeBudget.IsPositive() {
			return errors.New("enabled network requires positive transaction and daily network fee limits")
		}
	}
	return nil
}

func (p NetworkPolicy) ValidateForCapabilities(capabilities ChainFamilyCapabilities, contractAssetEnabled bool) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := capabilities.Validate(); err != nil {
		return err
	}
	if !p.Enabled {
		return nil
	}
	if !p.FeeFunding.Mode.Valid() {
		return errors.New("enabled network requires a valid fee funding mode")
	}
	if contractAssetEnabled && p.FeeFunding.Mode != FeeFundingJIT {
		return errors.New("enabled contract assets require JIT native fee funding")
	}
	if p.FeeFunding.Mode == FeeFundingJIT {
		if !p.FeeFunding.MaxFundingAmount.IsPositive() || !p.FeeFunding.DailyBudget.IsPositive() {
			return errors.New("JIT fee funding requires positive funding limits")
		}
		required := p.PerTransactionMaxFee.Add(p.FeeFunding.MinSourceWalletResidual)
		if p.FeeFunding.MaxFundingAmount.Cmp(required) < 0 {
			return fmt.Errorf("maximum JIT funding %s must cover fee cap and residual %s", p.FeeFunding.MaxFundingAmount, required)
		}
		if p.FeeFunding.DailyBudget.Cmp(p.FeeFunding.MaxFundingAmount) <= 0 {
			return errors.New("daily JIT funding budget must exceed one maximum funding amount")
		}
	} else if p.FeeFunding.MaxFundingAmount.IsPositive() || p.FeeFunding.DailyBudget.IsPositive() {
		return errors.New("source-only fee funding cannot configure JIT funding limits")
	}
	if capabilities.ExternalResourceMode == ExternalResourceNone {
		if p.ExternalResources != nil {
			return errors.New("chain family does not support an external resource policy")
		}
		return nil
	}
	return p.ExternalResources.Validate()
}

func (p NetworkPolicy) ResourceProviderRequired() bool {
	return p.ExternalResources != nil && p.ExternalResources.Strategy != ResourceBurnOnly
}

func (p NetworkPolicy) RequiresResourceRentalForQuote(quote FeeQuote) bool {
	return p.ExternalResources != nil &&
		p.ExternalResources.Strategy == ResourceRentalRequired &&
		len(quote.ResourceRequirements) > 0
}

// BoundFeeQuote applies the current runtime policy to a driver quote before
// the quote is reserved, authorized, and embedded into an unsigned
// transaction. The driver cap is a protocol safety ceiling; the network policy
// is the operator-controlled runtime ceiling and may be stricter.
// When external resource rental is strictly required for a contract quote,
// native fee burn is forbidden and quote.MaxFee only serves as the protocol
// execution limit (e.g. TRON TVM fee_limit for delegated energy).
func (p NetworkPolicy) BoundFeeQuote(quote FeeQuote) (FeeQuote, error) {
	if p.OperationalHold {
		return FeeQuote{}, fmt.Errorf("network is on operational hold: %s", p.OperationalHoldReason)
	}
	if !p.Enabled || !p.PerTransactionMaxFee.IsPositive() {
		return FeeQuote{}, errors.New("network fee policy is disabled or has no per-transaction cap")
	}
	if !quote.EstimatedFee.IsPositive() || !quote.MaxFee.IsPositive() || quote.EstimatedFee.Cmp(quote.MaxFee) > 0 {
		return FeeQuote{}, errors.New("invalid network fee quote")
	}
	if !p.RequiresResourceRentalForQuote(quote) {
		if quote.EstimatedFee.Cmp(p.PerTransactionMaxFee) > 0 {
			return FeeQuote{}, fmt.Errorf(
				"estimated network fee %s exceeds per-transaction policy cap %s",
				quote.EstimatedFee,
				p.PerTransactionMaxFee,
			)
		}
		if quote.MaxFee.Cmp(p.PerTransactionMaxFee) > 0 {
			return FeeQuote{}, fmt.Errorf(
				"maximum network fee %s exceeds per-transaction policy cap %s",
				quote.MaxFee,
				p.PerTransactionMaxFee,
			)
		}
	}
	return quote, nil
}

type RuntimeEndpointStatus struct {
	EndpointID      id.ID                       `json:"endpointId,omitempty"`
	AppliedRevision string                      `json:"appliedRevision,omitempty"`
	ProviderID      string                      `json:"providerId"`
	Role            string                      `json:"role"`
	State           string                      `json:"state"`
	Failures        int                         `json:"failures"`
	LastError       string                      `json:"lastError,omitempty"`
	Operations      []RuntimeRPCOperationStatus `json:"operations,omitempty"`
}

type RuntimeRPCOperationStatus struct {
	Operation  string     `json:"operation"`
	State      string     `json:"state"`
	Failures   int        `json:"failures"`
	ReasonCode string     `json:"reasonCode,omitempty"`
	RetryAt    *time.Time `json:"retryAt,omitempty"`
}

type NetworkRuntimeStatus struct {
	NetworkID             id.ID                   `json:"networkId"`
	RPCEndpoints          []RuntimeEndpointStatus `json:"rpcEndpoints"`
	SignerReady           bool                    `json:"signerReady"`
	SignerError           string                  `json:"signerError,omitempty"`
	ResourceProviderReady bool                    `json:"resourceProviderReady"`
	ResourceProviderState string                  `json:"resourceProviderState"`
	ResourceProviders     []RuntimeEndpointStatus `json:"resourceProviders,omitempty"`
	ObservedAt            time.Time               `json:"observedAt"`
	WorkerID              string                  `json:"workerId,omitempty"`
	OperationalHold       bool                    `json:"operationalHold"`
	OperationalHoldReason string                  `json:"operationalHoldReason,omitempty"`
}

func (s NetworkRuntimeStatus) Ready(now time.Time, capabilities ChainFamilyCapabilities, resourceRequired bool) bool {
	if s.OperationalHold || s.ObservedAt.IsZero() || now.Sub(s.ObservedAt) > time.Minute || !s.SignerReady {
		return false
	}
	roles := make(map[string]bool, len(capabilities.RequiredRPCRoles))
	for _, role := range capabilities.RequiredRPCRoles {
		roles[role] = false
	}
	for _, endpoint := range s.RPCEndpoints {
		if endpoint.State == "wrong_chain" {
			return false
		}
		if _, required := roles[endpoint.Role]; required && endpoint.State == "closed" {
			roles[endpoint.Role] = true
		}
	}
	for _, ready := range roles {
		if !ready {
			return false
		}
	}
	return !resourceRequired || s.ResourceProviderReady
}

// DriverRuntimeReporter is an optional operational capability. It exposes no
// endpoint URLs or credentials.
type DriverRuntimeReporter interface {
	RuntimeEndpoints() []RuntimeEndpointStatus
}

type DriverRuntimeProber interface {
	ProbeRuntime(ctx context.Context)
}

type NetworkPolicyState struct {
	Network      NetworkRef              `json:"network"`
	Capabilities ChainFamilyCapabilities `json:"capabilities"`
	Policy       NetworkPolicy           `json:"policy"`
	Runtime      NetworkRuntimeStatus    `json:"runtime"`
}

type NetworkPolicyPatch struct {
	Version                int
	Enabled                *bool
	PerTransactionMaxFee   *types.CryptoAmount
	DailyNetworkFeeBudget  *types.CryptoAmount
	FeeFunding             *FeeFundingPolicy
	ExternalResources      *ExternalResourcePolicy
	ClearExternalResources bool
	RetryDelay             *time.Duration
	RPCFailureThreshold    *int
	RPCOpenDuration        *time.Duration
}

func (p NetworkPolicy) ApplyPatch(patch NetworkPolicyPatch) NetworkPolicy {
	if p.ExternalResources != nil {
		resources := *p.ExternalResources
		resources.ProviderAccountIDs = append([]id.ID(nil), resources.ProviderAccountIDs...)
		p.ExternalResources = &resources
	}
	if patch.Enabled != nil {
		p.Enabled = *patch.Enabled
	}
	if patch.PerTransactionMaxFee != nil {
		p.PerTransactionMaxFee = *patch.PerTransactionMaxFee
	}
	if patch.DailyNetworkFeeBudget != nil {
		p.DailyNetworkFeeBudget = *patch.DailyNetworkFeeBudget
	}
	if patch.FeeFunding != nil {
		p.FeeFunding = *patch.FeeFunding
	}
	if patch.ClearExternalResources {
		p.ExternalResources = nil
	} else if patch.ExternalResources != nil {
		resources := *patch.ExternalResources
		resources.ProviderAccountIDs = append([]id.ID(nil), resources.ProviderAccountIDs...)
		p.ExternalResources = &resources
	}
	if patch.RetryDelay != nil {
		p.RetryDelay = *patch.RetryDelay
	}
	if patch.RPCFailureThreshold != nil {
		p.RPCFailureThreshold = *patch.RPCFailureThreshold
	}
	if patch.RPCOpenDuration != nil {
		p.RPCOpenDuration = *patch.RPCOpenDuration
	}
	return p
}

type NetworkPolicyAdminRepository interface {
	ListNetworkPolicyStates(ctx context.Context) ([]NetworkPolicyState, error)
	GetNetworkPolicyState(ctx context.Context, networkID id.ID) (NetworkPolicyState, error)
	NetworkFundingSourcesReady(ctx context.Context, networkID id.ID, minimum types.CryptoAmount) (bool, error)
	NetworkHasActiveContractAssets(ctx context.Context, networkID id.ID) (bool, error)
	NetworkProviderAccountCodes(ctx context.Context, networkID id.ID, accountIDs []id.ID, observedAt time.Time) (map[id.ID]string, error)
	UpdateNetworkPolicy(ctx context.Context, policy NetworkPolicy, expectedVersion int, actor string) (NetworkPolicyState, error)
}

type FeeStrategy string

const (
	FeeStrategyResourceRental FeeStrategy = "resource_rental"
	FeeStrategyNativeBurn     FeeStrategy = "native_burn"
	FeeStrategyNativeFunding  FeeStrategy = "native_funding"
)

func (s FeeStrategy) Valid() bool {
	return s == FeeStrategyResourceRental || s == FeeStrategyNativeBurn || s == FeeStrategyNativeFunding
}

type FeeReservationStatus string

const (
	FeeReservationReserved  FeeReservationStatus = "reserved"
	FeeReservationBroadcast FeeReservationStatus = "broadcast"
	FeeReservationSettled   FeeReservationStatus = "settled"
	FeeReservationReleased  FeeReservationStatus = "released"
	FeeReservationHeld      FeeReservationStatus = "held"
)

type FeeReservation struct {
	ID                  id.ID
	NetworkID           id.ID
	PayerWalletID       id.ID
	NativeTokenID       id.ID
	OperationTokenID    id.ID
	ReservedPrincipal   types.CryptoAmount
	BasisType           string
	BasisID             id.ID
	Purpose             SigningPurpose
	Attempt             int
	Strategy            FeeStrategy
	MaxFee              types.CryptoAmount
	ActualFee           types.CryptoAmount
	UnsignedFingerprint string
	TxHash              string
	Status              FeeReservationStatus
	HoldReason          string
	ReservedAt          time.Time
}
