package crypto

import (
	"errors"
	"testing"
)

type capabilityProviderStub struct {
	family       ChainFamily
	capabilities ChainFamilyCapabilities
}

func (p capabilityProviderStub) Family() ChainFamily { return p.family }
func (p capabilityProviderStub) Capabilities() ChainFamilyCapabilities {
	return p.capabilities
}

func TestChainFamilyCapabilityRegistry(t *testing.T) {
	provider := capabilityProviderStub{
		family:       ChainFamilyEVM,
		capabilities: evmWalletPoolCapabilities(),
	}
	registry, err := BuildChainFamilyCapabilityRegistry([]ChainFamilyCapabilityProvider{provider})
	if err != nil {
		t.Fatalf("BuildChainFamilyCapabilityRegistry() error = %v", err)
	}
	capabilities, err := registry.ResolveCapabilities(ChainFamilyEVM)
	if err != nil {
		t.Fatalf("ResolveCapabilities() error = %v", err)
	}
	if !capabilities.Equal(provider.capabilities) {
		t.Fatalf("capabilities = %#v, want %#v", capabilities, provider.capabilities)
	}
	_, err = registry.ResolveCapabilities(ChainFamilyTON)
	if !errors.Is(err, ErrChainFamilyCapabilitiesNotFound) {
		t.Fatalf("ResolveCapabilities() error = %v, want not found", err)
	}
}

func TestChainFamilyCapabilityRegistryRejectsDuplicateAndIdentityMismatch(t *testing.T) {
	provider := capabilityProviderStub{
		family:       ChainFamilyEVM,
		capabilities: evmWalletPoolCapabilities(),
	}
	_, err := BuildChainFamilyCapabilityRegistry([]ChainFamilyCapabilityProvider{provider, provider})
	if !errors.Is(err, ErrDuplicateChainFamilyCapabilities) {
		t.Fatalf("duplicate provider error = %v", err)
	}

	mismatch := provider
	mismatch.capabilities.Family = ChainFamilyTRON
	if _, err := BuildChainFamilyCapabilityRegistry([]ChainFamilyCapabilityProvider{mismatch}); err == nil {
		t.Fatal("identity mismatch was accepted")
	}
}

func TestChainFamilyCapabilitiesValidateRequiresRailMetadata(t *testing.T) {
	valid := evmWalletPoolCapabilities()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid capabilities rejected: %v", err)
	}
	if !valid.RequiresNetworkSequence() {
		t.Fatal("account sequence should require a network sequence")
	}
	if tronWalletPoolCapabilities().RequiresNetworkSequence() {
		t.Fatal("sequence-none family should not require a network sequence")
	}

	missing := valid
	missing.SequenceModel = ""
	if err := missing.Validate(); err == nil {
		t.Fatal("missing sequence model was accepted")
	}
	missing = valid
	missing.CheckpointTimeUnit = ""
	if err := missing.Validate(); err == nil {
		t.Fatal("missing checkpoint time unit was accepted")
	}
	missing = valid
	missing.TokenAccountModel = ""
	if err := missing.Validate(); err == nil {
		t.Fatal("missing token account model was accepted")
	}
	missing = valid
	missing.MinWatcherLagSeconds = -1
	if err := missing.Validate(); err == nil {
		t.Fatal("negative watcher lag was accepted")
	}
}
