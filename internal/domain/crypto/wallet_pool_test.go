package crypto

import (
	"testing"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func validWalletPoolPolicy() WalletPoolPolicy {
	return WalletPoolPolicy{
		NetworkID:                          id.New(),
		ChainFamily:                        ChainFamilyTRON,
		Enabled:                            true,
		LowWatermark:                       2,
		TargetReady:                        5,
		MaxTotalWallets:                    20,
		MaxProvisionBatch:                  5,
		MaxActiveInvoiceLeasesPerMerchant:  5,
		MaxPersistentAddressesPerMerchant:  5,
		MaxLeaseStartsPerMinutePerMerchant: 20,
		MinGlobalReadyReserve:              1,
		Activation: &WalletPoolActivationPolicy{
			MaxConcurrent:      1,
			ReserveMinor:       types.NewRawAmountFromInt64(2_000_000),
			MinHotBalanceMinor: types.NewRawAmountFromInt64(2_000_000),
			DailyBudgetMinor:   types.NewRawAmountFromInt64(10_000_000),
		},
		RetryAfterSeconds:   30,
		DerivationNamespace: "m/44'/195'/42'/0",
		Version:             1,
	}
}

func tronWalletPoolCapabilities() ChainFamilyCapabilities {
	return ChainFamilyCapabilities{
		Family:               ChainFamilyTRON,
		WalletProvisioning:   WalletProvisioningDerivedWithActivation,
		RequiredRPCRoles:     []string{"indexer", "fullnode", "solidity"},
		ExternalResourceMode: ExternalResourceOptional,
		SequenceModel:        SequenceNone,
		CheckpointTimeUnit:   CheckpointUnixMillis,
		TokenAccountModel:    TokenAccountSameAddress,
		MinWatcherLagSeconds: 180,
	}
}

func evmWalletPoolCapabilities() ChainFamilyCapabilities {
	return ChainFamilyCapabilities{
		Family:               ChainFamilyEVM,
		WalletProvisioning:   WalletProvisioningDerived,
		RequiredRPCRoles:     []string{"rpc"},
		ExternalResourceMode: ExternalResourceNone,
		SequenceModel:        SequenceAccount,
		CheckpointTimeUnit:   CheckpointUnixSeconds,
		TokenAccountModel:    TokenAccountSameAddress,
		SupportsReplacement:  true,
	}
}

func TestWalletPoolPolicyValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WalletPoolPolicy)
	}{
		{"low is negative", func(p *WalletPoolPolicy) { p.LowWatermark = -1 }},
		{"low equals target", func(p *WalletPoolPolicy) { p.LowWatermark = p.TargetReady }},
		{"target exceeds maximum", func(p *WalletPoolPolicy) { p.TargetReady = p.MaxTotalWallets + 1 }},
		{"batch is zero", func(p *WalletPoolPolicy) { p.MaxProvisionBatch = 0 }},
		{"concurrency is zero", func(p *WalletPoolPolicy) { p.Activation.MaxConcurrent = 0 }},
		{"active invoice quota is zero", func(p *WalletPoolPolicy) { p.MaxActiveInvoiceLeasesPerMerchant = 0 }},
		{"persistent address quota is zero", func(p *WalletPoolPolicy) { p.MaxPersistentAddressesPerMerchant = 0 }},
		{"lease rate is zero", func(p *WalletPoolPolicy) { p.MaxLeaseStartsPerMinutePerMerchant = 0 }},
		{"global reserve is negative", func(p *WalletPoolPolicy) { p.MinGlobalReadyReserve = -1 }},
		{"global reserve exceeds low watermark", func(p *WalletPoolPolicy) { p.MinGlobalReadyReserve = p.LowWatermark + 1 }},
		{"retry after is zero", func(p *WalletPoolPolicy) { p.RetryAfterSeconds = 0 }},
		{"cooldown without lag", func(p *WalletPoolPolicy) { p.AddressReuseCooldownSeconds = 1800 }},
		{"tron cooldown lag below floor", func(p *WalletPoolPolicy) {
			p.AddressReuseCooldownSeconds = 1800
			p.MaxWatcherLagSeconds = 60
		}},
		{"enabled generation needs review", func(p *WalletPoolPolicy) { p.NeedsReview = true }},
		{"enabled generation is empty", func(p *WalletPoolPolicy) { p.DerivationNamespace = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := validWalletPoolPolicy()
			test.mutate(&policy)
			if err := policy.Validate(tronWalletPoolCapabilities()); err == nil {
				t.Fatal("Validate() returned nil")
			}
		})
	}

	if err := validWalletPoolPolicy().Validate(tronWalletPoolCapabilities()); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}

	enabled := validWalletPoolPolicy()
	enabled.AddressReuseCooldownSeconds = 1800
	enabled.MaxWatcherLagSeconds = 900
	if err := enabled.Validate(tronWalletPoolCapabilities()); err != nil {
		t.Fatalf("tron cooldown policy rejected: %v", err)
	}

	evmCooldown := validWalletPoolPolicy()
	evmCooldown.ChainFamily = ChainFamilyEVM
	evmCooldown.Activation = nil
	evmCooldown.AddressReuseCooldownSeconds = 1800
	evmCooldown.MaxWatcherLagSeconds = 1
	if err := evmCooldown.Validate(evmWalletPoolCapabilities()); err != nil {
		t.Fatalf("evm cooldown policy rejected: %v", err)
	}
}

func TestWalletPoolPolicyValidationFollowsProvisioningCapabilities(t *testing.T) {
	derived := validWalletPoolPolicy()
	derived.ChainFamily = ChainFamilyEVM
	derived.Activation = nil
	if err := derived.Validate(evmWalletPoolCapabilities()); err != nil {
		t.Fatalf("derived policy rejected: %v", err)
	}

	derived.Activation = &WalletPoolActivationPolicy{MaxConcurrent: 1}
	if err := derived.Validate(evmWalletPoolCapabilities()); err == nil {
		t.Fatal("derived policy accepted an activation-only subsection")
	}

	unsupported := derived
	unsupported.Activation = nil
	unsupportedCaps := evmWalletPoolCapabilities()
	unsupportedCaps.WalletProvisioning = WalletProvisioningUnsupported
	if err := unsupported.Validate(unsupportedCaps); err == nil {
		t.Fatal("enabled policy accepted unsupported provisioning")
	}

	activationRequired := validWalletPoolPolicy()
	activationRequired.Activation = nil
	if err := activationRequired.Validate(tronWalletPoolCapabilities()); err == nil {
		t.Fatal("enabled activation-based policy accepted no activation subsection")
	}
}

func TestComputeWalletPoolState(t *testing.T) {
	policy := validWalletPoolPolicy()
	if got := ComputeWalletPoolState(policy, WalletPoolStats{Ready: 5}); got != WalletPoolStateHealthy {
		t.Fatalf("healthy state = %q", got)
	}
	if got := ComputeWalletPoolState(policy, WalletPoolStats{Ready: 2}); got != WalletPoolStateLow {
		t.Fatalf("low state = %q", got)
	}
	if got := ComputeWalletPoolState(policy, WalletPoolStats{Ready: 0}); got != WalletPoolStateDepleted {
		t.Fatalf("depleted state = %q", got)
	}
	policy.Enabled = false
	if got := ComputeWalletPoolState(policy, WalletPoolStats{Ready: 5}); got != WalletPoolStatePaused {
		t.Fatalf("paused state = %q", got)
	}
}

func TestResolveMerchantWalletAllocationLimitsUsesOverridesAndDefaults(t *testing.T) {
	policy := validWalletPoolPolicy()
	active := 9
	limits := ResolveMerchantWalletAllocationLimits(policy, MerchantWalletAllocationOverride{
		MerchantID:             id.New(),
		NetworkID:              policy.NetworkID,
		MaxActiveInvoiceLeases: &active,
		Version:                2,
	})
	if limits.EffectiveMaxActiveInvoiceLeases != active {
		t.Fatalf("active limit = %d, want %d", limits.EffectiveMaxActiveInvoiceLeases, active)
	}
	if limits.EffectiveMaxPersistentAddresses != policy.MaxPersistentAddressesPerMerchant {
		t.Fatalf("persistent limit = %d, want inherited %d", limits.EffectiveMaxPersistentAddresses, policy.MaxPersistentAddressesPerMerchant)
	}
	if limits.EffectiveMaxLeaseStartsPerMinute != policy.MaxLeaseStartsPerMinutePerMerchant {
		t.Fatalf("rate limit = %d, want inherited %d", limits.EffectiveMaxLeaseStartsPerMinute, policy.MaxLeaseStartsPerMinutePerMerchant)
	}
}

func TestMerchantWalletAllocationOverrideValidation(t *testing.T) {
	zero := 0
	if err := (MerchantWalletAllocationOverride{MaxPersistentAddresses: &zero}).Validate(); err == nil {
		t.Fatal("Validate() returned nil for zero persistent quota")
	}
	if err := (MerchantWalletAllocationOverride{Version: -1}).Validate(); err == nil {
		t.Fatal("Validate() returned nil for negative version")
	}
}

func TestValidateWalletDerivationRejectsPathMismatch(t *testing.T) {
	job := WalletProvisioningJob{DerivationPath: "m/44'/195'/42'/0/3"}
	result := WalletDerivation{
		Address:        "TAddress",
		VaultKeyID:     "vault/key",
		DerivationPath: "m/44'/195'/42'/0/4",
	}
	if err := ValidateWalletDerivation(job, result); err == nil {
		t.Fatal("expected derivation path mismatch")
	}
}
