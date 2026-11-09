package crypto

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// WalletProvisioningMode describes how a family materializes receiving
// wallets. It is executable adapter metadata, not an operator-editable policy.
type WalletProvisioningMode string

const (
	WalletProvisioningUnsupported           WalletProvisioningMode = "unsupported"
	WalletProvisioningDerived               WalletProvisioningMode = "derived"
	WalletProvisioningDerivedWithActivation WalletProvisioningMode = "derived_with_activation"
)

func (m WalletProvisioningMode) Valid() bool {
	switch m {
	case WalletProvisioningUnsupported, WalletProvisioningDerived, WalletProvisioningDerivedWithActivation:
		return true
	default:
		return false
	}
}

func (m WalletProvisioningMode) Supported() bool {
	return m == WalletProvisioningDerived || m == WalletProvisioningDerivedWithActivation
}

func (m WalletProvisioningMode) RequiresActivation() bool {
	return m == WalletProvisioningDerivedWithActivation
}

// SequenceModel is how a family owns outbound transaction ordering.
type SequenceModel string

const (
	SequenceNone    SequenceModel = "none"
	SequenceAccount SequenceModel = "account"
)

func (m SequenceModel) Valid() bool {
	return m == SequenceNone || m == SequenceAccount
}

// CheckpointTimeUnit is the unit of WatcherState.LastTimestamp.
type CheckpointTimeUnit string

const (
	CheckpointUnixMillis  CheckpointTimeUnit = "unix_millis"
	CheckpointUnixSeconds CheckpointTimeUnit = "unix_seconds"
)

func (u CheckpointTimeUnit) Valid() bool {
	return u == CheckpointUnixMillis || u == CheckpointUnixSeconds
}

// TokenAccountModel describes whether a receiving address is enough to hold a
// token, or whether a derived per-asset account must exist (ATA, jetton wallet).
type TokenAccountModel string

const (
	TokenAccountSameAddress     TokenAccountModel = "same_address"
	TokenAccountDerivedPerAsset TokenAccountModel = "derived_per_asset"
)

func (m TokenAccountModel) Valid() bool {
	return m == TokenAccountSameAddress || m == TokenAccountDerivedPerAsset
}

// ChainFamilyCapabilities is the compile-time contract exported by one family
// adapter. Runtime readiness and per-network operator policy are intentionally
// separate concerns.
type ChainFamilyCapabilities struct {
	Family               ChainFamily            `json:"family"`
	WalletProvisioning   WalletProvisioningMode `json:"walletProvisioning"`
	RequiredRPCRoles     []string               `json:"requiredRpcRoles"`
	ExternalResourceMode ExternalResourceMode   `json:"externalResourceMode"`
	SequenceModel        SequenceModel          `json:"sequenceModel"`
	CheckpointTimeUnit   CheckpointTimeUnit     `json:"checkpointTimeUnit"`
	TokenAccountModel    TokenAccountModel      `json:"tokenAccountModel"`
	MinWatcherLagSeconds int                    `json:"minWatcherLagSeconds"`
	SupportsReplacement  bool                   `json:"supportsReplacement"`
}

// ExternalResourceMode reports whether an adapter can satisfy protocol costs
// through a chain-external resource provider (for example TRON Energy rental).
// Native fee funding is intentionally not represented here: it is a common
// money-movement policy and applies to EVM token transfers as well.
type ExternalResourceMode string

const (
	ExternalResourceNone     ExternalResourceMode = "none"
	ExternalResourceOptional ExternalResourceMode = "optional"
)

func (m ExternalResourceMode) Valid() bool {
	return m == ExternalResourceNone || m == ExternalResourceOptional
}

func (c ChainFamilyCapabilities) Validate() error {
	if !c.Family.Valid() {
		return errors.New("capabilities require a valid chain family")
	}
	if !c.WalletProvisioning.Valid() {
		return errors.New("capabilities require a valid wallet provisioning mode")
	}
	if !c.ExternalResourceMode.Valid() {
		return errors.New("capabilities require a valid external resource mode")
	}
	if !c.SequenceModel.Valid() {
		return errors.New("capabilities require a valid sequence model")
	}
	if !c.CheckpointTimeUnit.Valid() {
		return errors.New("capabilities require a valid checkpoint time unit")
	}
	if !c.TokenAccountModel.Valid() {
		return errors.New("capabilities require a valid token account model")
	}
	if c.MinWatcherLagSeconds < 0 {
		return errors.New("capabilities min watcher lag cannot be negative")
	}
	if len(c.RequiredRPCRoles) == 0 {
		return errors.New("capabilities require at least one RPC role")
	}
	seen := make(map[string]struct{}, len(c.RequiredRPCRoles))
	for _, role := range c.RequiredRPCRoles {
		role = strings.TrimSpace(role)
		if role == "" {
			return errors.New("capabilities RPC roles cannot be empty")
		}
		if _, duplicate := seen[role]; duplicate {
			return fmt.Errorf("capabilities RPC role %q is duplicated", role)
		}
		seen[role] = struct{}{}
	}
	return nil
}

// Equal performs a semantic comparison. Capabilities contain slices and must
// never be compared through struct identity.
func (c ChainFamilyCapabilities) Equal(other ChainFamilyCapabilities) bool {
	return c.Family == other.Family &&
		c.WalletProvisioning == other.WalletProvisioning &&
		c.ExternalResourceMode == other.ExternalResourceMode &&
		c.SequenceModel == other.SequenceModel &&
		c.CheckpointTimeUnit == other.CheckpointTimeUnit &&
		c.TokenAccountModel == other.TokenAccountModel &&
		c.MinWatcherLagSeconds == other.MinWatcherLagSeconds &&
		c.SupportsReplacement == other.SupportsReplacement &&
		slices.Equal(c.RequiredRPCRoles, other.RequiredRPCRoles)
}

func (c ChainFamilyCapabilities) RequiresNetworkSequence() bool {
	return c.SequenceModel == SequenceAccount
}

func (c ChainFamilyCapabilities) LagHints() WatcherLagHints {
	return WatcherLagHints{TimeUnit: c.CheckpointTimeUnit}
}

type ChainFamilyCapabilityProvider interface {
	Family() ChainFamily
	Capabilities() ChainFamilyCapabilities
}

type ChainFamilyCapabilityResolver interface {
	ResolveCapabilities(family ChainFamily) (ChainFamilyCapabilities, error)
}

var (
	ErrChainFamilyCapabilitiesNotFound  = errors.New("chain family capabilities not found")
	ErrDuplicateChainFamilyCapabilities = errors.New("chain family capabilities already registered")
)

// ChainFamilyCapabilityRegistry is immutable after composition. Both the API
// and worker receive the same registry so enablement and execution cannot use
// different family-support rules.
type ChainFamilyCapabilityRegistry struct {
	byFamily map[ChainFamily]ChainFamilyCapabilities
}

func BuildChainFamilyCapabilityRegistry(providers []ChainFamilyCapabilityProvider) (*ChainFamilyCapabilityRegistry, error) {
	registry := &ChainFamilyCapabilityRegistry{byFamily: make(map[ChainFamily]ChainFamilyCapabilities, len(providers))}
	for _, provider := range providers {
		if provider == nil {
			return nil, errors.New("chain family capability provider is required")
		}
		capabilities := provider.Capabilities()
		if err := capabilities.Validate(); err != nil {
			return nil, fmt.Errorf("family %s capabilities: %w", provider.Family(), err)
		}
		if capabilities.Family != provider.Family() {
			return nil, fmt.Errorf("family capability provider identity mismatch: provider=%s capabilities=%s", provider.Family(), capabilities.Family)
		}
		if _, exists := registry.byFamily[capabilities.Family]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateChainFamilyCapabilities, capabilities.Family)
		}
		registry.byFamily[capabilities.Family] = capabilities
	}
	return registry, nil
}

func (r *ChainFamilyCapabilityRegistry) ResolveCapabilities(family ChainFamily) (ChainFamilyCapabilities, error) {
	if r == nil || !family.Valid() {
		return ChainFamilyCapabilities{}, fmt.Errorf("%w: %s", ErrChainFamilyCapabilitiesNotFound, family)
	}
	capabilities, ok := r.byFamily[family]
	if !ok {
		return ChainFamilyCapabilities{}, fmt.Errorf("%w: %s", ErrChainFamilyCapabilitiesNotFound, family)
	}
	return capabilities, nil
}

var _ ChainFamilyCapabilityResolver = (*ChainFamilyCapabilityRegistry)(nil)
