package crypto

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/eventlog"
)

// WalletPoolAdminService owns all synchronous wallet-pool use cases. It keeps
// HTTP transport and PostgreSQL adapters free of business orchestration.
type WalletPoolAdminService struct {
	repo         WalletPoolAdminRepository
	capabilities ChainFamilyCapabilityResolver
	eventWriter  eventlog.Writer
}

func NewWalletPoolAdminService(
	repo WalletPoolAdminRepository,
	capabilities ChainFamilyCapabilityResolver,
	eventWriter eventlog.Writer,
) *WalletPoolAdminService {
	if repo == nil {
		panic("crypto.NewWalletPoolAdminService: repo is required")
	}
	if eventWriter == nil {
		panic("crypto.NewWalletPoolAdminService: event writer is required")
	}
	if capabilities == nil {
		panic("crypto.NewWalletPoolAdminService: chain family capabilities are required")
	}
	return &WalletPoolAdminService{repo: repo, capabilities: capabilities, eventWriter: eventWriter}
}

func (s *WalletPoolAdminService) List(ctx context.Context) ([]WalletPoolState, error) {
	states, err := s.repo.ListStates(ctx)
	if err != nil {
		return nil, err
	}
	for i := range states {
		states[i], err = s.decorateState(states[i])
		if err != nil {
			return nil, err
		}
	}
	return states, nil
}

func (s *WalletPoolAdminService) UpdatePolicy(
	ctx context.Context,
	networkID id.ID,
	patch WalletPoolPolicyPatch,
) (WalletPoolState, error) {
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return WalletPoolState{}, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}

	var updated WalletPoolState
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		current, err := s.repo.GetState(ctx, networkID)
		if err != nil {
			return err
		}
		policy := current.Policy.ApplyPatch(patch)
		capabilities, err := s.capabilitiesFor(policy.ChainFamily)
		if err != nil {
			return err
		}
		if err := policy.Validate(capabilities); err != nil {
			return err
		}
		if policy.Enabled && capabilities.WalletProvisioning.RequiresActivation() {
			funded, err := s.repo.HasConfirmedNativeFunding(ctx, networkID)
			if err != nil {
				return fmt.Errorf("check wallet pool native funding: %w", err)
			}
			if !funded {
				return apperror.NewValidation(
					"confirmed native hot-wallet funding receipt is required before enabling the wallet pool",
				).WithDetail("networkCode", policy.NetworkCode)
			}
		}

		updated, err = s.repo.UpdatePolicy(ctx, policy, walletPoolActor(ctx))
		if err != nil {
			return err
		}
		updated.Capabilities = capabilities
		network := networkID
		if err := s.eventWriter.Write(ctx, eventlog.Event{
			Category:   eventlog.CategoryData,
			Severity:   eventlog.SeverityInfo,
			EventType:  eventlog.EventType("crypto.wallet_pool.policy_updated"),
			Source:     "api",
			EntityType: "blockchain_network",
			EntityID:   &network,
			Message:    "Crypto wallet pool policy updated",
			Details: map[string]any{
				"networkId":                          network.String(),
				"previousVersion":                    current.Policy.Version,
				"version":                            updated.Policy.Version,
				"enabled":                            updated.Policy.Enabled,
				"lowWatermark":                       updated.Policy.LowWatermark,
				"targetReady":                        updated.Policy.TargetReady,
				"maxTotalWallets":                    updated.Policy.MaxTotalWallets,
				"maxActiveInvoiceLeasesPerMerchant":  updated.Policy.MaxActiveInvoiceLeasesPerMerchant,
				"maxPersistentAddressesPerMerchant":  updated.Policy.MaxPersistentAddressesPerMerchant,
				"maxLeaseStartsPerMinutePerMerchant": updated.Policy.MaxLeaseStartsPerMinutePerMerchant,
				"minGlobalReadyReserve":              updated.Policy.MinGlobalReadyReserve,
			},
		}); err != nil {
			return fmt.Errorf("write wallet pool policy audit event: %w", err)
		}
		return nil
	})
	return updated, err
}

func (s *WalletPoolAdminService) GetMerchantAllocationLimits(
	ctx context.Context,
	merchantID, networkID id.ID,
) (MerchantWalletAllocationLimits, error) {
	state, err := s.repo.GetState(ctx, networkID)
	if err != nil {
		return MerchantWalletAllocationLimits{}, err
	}
	override, _, err := s.repo.GetMerchantAllocationOverride(ctx, merchantID, networkID)
	if err != nil {
		return MerchantWalletAllocationLimits{}, err
	}
	return ResolveMerchantWalletAllocationLimits(state.Policy, override), nil
}

func (s *WalletPoolAdminService) UpdateMerchantAllocationLimits(
	ctx context.Context,
	merchantID, networkID id.ID,
	override MerchantWalletAllocationOverride,
) (MerchantWalletAllocationLimits, error) {
	override.MerchantID = merchantID
	override.NetworkID = networkID
	if err := override.Validate(); err != nil {
		return MerchantWalletAllocationLimits{}, err
	}

	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return MerchantWalletAllocationLimits{}, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}

	var policy WalletPoolPolicy
	var saved MerchantWalletAllocationOverride
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		state, err := s.repo.GetState(ctx, networkID)
		if err != nil {
			return err
		}
		policy = state.Policy

		current, exists, err := s.repo.GetMerchantAllocationOverride(ctx, merchantID, networkID)
		if err != nil {
			return err
		}
		if (!exists && override.Version != 0) || (exists && current.Version != override.Version) {
			return apperror.NewConcurrentModification(
				"merchant_wallet_allocation_override",
				merchantID.String()+":"+networkID.String(),
			)
		}

		saved, err = s.repo.UpsertMerchantAllocationOverride(ctx, override, walletPoolActor(ctx))
		if err != nil {
			return err
		}
		merchant := merchantID
		return s.eventWriter.Write(ctx, eventlog.Event{
			Category:   eventlog.CategoryData,
			Severity:   eventlog.SeverityInfo,
			EventType:  eventlog.EventType("crypto.wallet_pool.merchant_override_updated"),
			Source:     "api",
			EntityType: "merchant",
			EntityID:   &merchant,
			Message:    "Merchant wallet allocation override updated",
			Details: map[string]any{
				"networkId":               networkID.String(),
				"previousVersion":         current.Version,
				"version":                 saved.Version,
				"maxActiveInvoiceLeases":  saved.MaxActiveInvoiceLeases,
				"maxPersistentAddresses":  saved.MaxPersistentAddresses,
				"maxLeaseStartsPerMinute": saved.MaxLeaseStartsPerMinute,
			},
		})
	})
	if err != nil {
		return MerchantWalletAllocationLimits{}, err
	}
	return ResolveMerchantWalletAllocationLimits(policy, saved), nil
}

func (s *WalletPoolAdminService) Reconcile(
	ctx context.Context,
	networkID id.ID,
) ([]WalletPoolPlanResult, WalletPoolState, error) {
	state, err := s.repo.GetState(ctx, networkID)
	if err != nil {
		return nil, WalletPoolState{}, err
	}
	state, err = s.decorateState(state)
	if err != nil {
		return nil, WalletPoolState{}, err
	}
	if err := state.Policy.Validate(state.Capabilities); err != nil {
		return nil, WalletPoolState{}, err
	}
	if !state.Policy.Enabled {
		return nil, WalletPoolState{}, apperror.NewValidation("wallet pool policy is disabled")
	}
	result, err := s.repo.PlanNetworkRefill(ctx, networkID)
	if err != nil {
		return nil, WalletPoolState{}, err
	}
	state, err = s.repo.GetState(ctx, networkID)
	if err != nil {
		return nil, WalletPoolState{}, err
	}
	state, err = s.decorateState(state)
	if err != nil {
		return nil, WalletPoolState{}, err
	}
	return []WalletPoolPlanResult{result}, state, nil
}

func (s *WalletPoolAdminService) decorateState(state WalletPoolState) (WalletPoolState, error) {
	capabilities, err := s.capabilitiesFor(state.Policy.ChainFamily)
	if err != nil {
		return WalletPoolState{}, err
	}
	state.Capabilities = capabilities
	return state, nil
}

func (s *WalletPoolAdminService) capabilitiesFor(family ChainFamily) (ChainFamilyCapabilities, error) {
	capabilities, err := s.capabilities.ResolveCapabilities(family)
	if errors.Is(err, ErrChainFamilyCapabilitiesNotFound) {
		return ChainFamilyCapabilities{
			Family:               family,
			WalletProvisioning:   WalletProvisioningUnsupported,
			RequiredRPCRoles:     []string{"unsupported"},
			ExternalResourceMode: ExternalResourceNone,
			SequenceModel:        SequenceNone,
			CheckpointTimeUnit:   CheckpointUnixSeconds,
			TokenAccountModel:    TokenAccountSameAddress,
		}, nil
	}
	if err != nil {
		return ChainFamilyCapabilities{}, fmt.Errorf("resolve chain family capabilities: %w", err)
	}
	return capabilities, nil
}

func walletPoolActor(ctx context.Context) string {
	user := appctx.GetUser(ctx)
	if user == nil {
		return "system"
	}
	if actor := strings.TrimSpace(user.Email); actor != "" {
		return actor
	}
	if actor := strings.TrimSpace(user.UserID); actor != "" {
		return actor
	}
	return "system"
}
