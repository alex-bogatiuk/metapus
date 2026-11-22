package crypto

import (
	"context"
	"fmt"
	"time"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/eventlog"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/tenant"
)

type NetworkPolicyAdminService struct {
	repo         NetworkPolicyAdminRepository
	capabilities ChainFamilyCapabilityResolver
	eventWriter  eventlog.Writer
	now          func() time.Time
}

func NewNetworkPolicyAdminService(repo NetworkPolicyAdminRepository, capabilities ChainFamilyCapabilityResolver, writer eventlog.Writer) *NetworkPolicyAdminService {
	if repo == nil || capabilities == nil || writer == nil {
		panic("crypto.NewNetworkPolicyAdminService: repository, capability resolver, and event writer are required")
	}
	return &NetworkPolicyAdminService{repo: repo, capabilities: capabilities, eventWriter: writer, now: time.Now}
}

func (s *NetworkPolicyAdminService) List(ctx context.Context) ([]NetworkPolicyState, error) {
	states, err := s.repo.ListNetworkPolicyStates(ctx)
	if err != nil {
		return nil, err
	}
	for i := range states {
		states[i], err = s.decorate(states[i])
		if err != nil {
			return nil, err
		}
	}
	return states, nil
}

func (s *NetworkPolicyAdminService) Get(ctx context.Context, networkID id.ID) (NetworkPolicyState, error) {
	state, err := s.repo.GetNetworkPolicyState(ctx, networkID)
	if err != nil {
		return state, err
	}
	return s.decorate(state)
}

func (s *NetworkPolicyAdminService) decorate(state NetworkPolicyState) (NetworkPolicyState, error) {
	capabilities, err := s.capabilities.ResolveCapabilities(state.Network.Family)
	if err != nil {
		return state, fmt.Errorf("resolve %s capabilities: %w", state.Network.Family, err)
	}
	state.Capabilities = capabilities
	return state, nil
}

func (s *NetworkPolicyAdminService) Update(ctx context.Context, networkID id.ID, patch NetworkPolicyPatch) (NetworkPolicyState, error) {
	if patch.Version <= 0 {
		return NetworkPolicyState{}, apperror.NewValidation("positive policy version is required")
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return NetworkPolicyState{}, apperror.NewInternal(err)
	}
	var updated NetworkPolicyState
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		current, err := s.repo.GetNetworkPolicyState(txCtx, networkID)
		if err != nil {
			return err
		}
		if current.Policy.Version != patch.Version {
			return apperror.NewConcurrentModification("crypto_network_policy", networkID.String())
		}
		current, err = s.decorate(current)
		if err != nil {
			return err
		}
		policy := current.Policy.ApplyPatch(patch)
		contractAssets, err := s.repo.NetworkHasActiveContractAssets(txCtx, networkID)
		if err != nil {
			return err
		}
		if err := policy.ValidateForCapabilities(current.Capabilities, contractAssets); err != nil {
			return apperror.NewValidation("invalid crypto network policy").WithCause(err)
		}
		if policy.Enabled && !current.Watching {
			return apperror.NewValidation("operations processing requires a watched network")
		}
		if policy.Enabled {
			if policy.FeeFunding.Mode == FeeFundingJIT {
				ready, err := s.repo.NetworkFundingSourcesReady(txCtx, networkID, policy.FeeFunding.MinFeeWalletBalance)
				if err != nil {
					return err
				}
				if !ready {
					return apperror.NewValidation("network fee sources are not ready for every active custody owner")
				}
			}
			if !current.Runtime.Ready(s.now().UTC(), current.Capabilities, false) {
				return apperror.NewValidation("fresh healthy RPC and signer status is required before enabling the network")
			}
			if policy.ResourceProviderRequired() {
				accountIDs := policy.ExternalResources.ProviderAccountIDs
				codes, err := s.repo.NetworkProviderAccountCodes(txCtx, networkID, accountIDs, current.Runtime.ObservedAt)
				if err != nil {
					return err
				}
				if len(codes) != len(accountIDs) {
					return apperror.NewValidation("selected resource provider accounts must be active, belong to the network, and have credentials")
				}
				ready := false
				for _, accountID := range accountIDs {
					for _, status := range current.Runtime.ResourceProviders {
						if status.ProviderID == codes[accountID] && status.State == "closed" {
							ready = true
							break
						}
					}
				}
				if !ready {
					return apperror.NewValidation("fresh healthy status for a selected resource provider account is required before enabling the network")
				}
			}
		}
		updated, err = s.repo.UpdateNetworkPolicy(txCtx, policy, patch.Version, walletPoolActor(txCtx))
		if err != nil {
			return err
		}
		network := networkID
		if err := s.eventWriter.Write(txCtx, eventlog.Event{
			Category: eventlog.CategorySecurity, Severity: eventlog.SeverityWarning,
			EventType: eventlog.EventType("crypto.network.policy_updated"), Source: "api",
			EntityType: "blockchain_network", EntityID: &network, Message: "Crypto network policy updated",
			Details: map[string]any{
				"previousVersion": current.Policy.Version, "version": updated.Policy.Version,
				"enabled": updated.Policy.Enabled, "feeFundingMode": updated.Policy.FeeFunding.Mode,
				"perTransactionMaxFee":  updated.Policy.PerTransactionMaxFee,
				"dailyNetworkFeeBudget": updated.Policy.DailyNetworkFeeBudget, "dailyJITFundingBudget": updated.Policy.FeeFunding.DailyBudget,
			},
		}); err != nil {
			return fmt.Errorf("write crypto network policy audit event: %w", err)
		}
		return nil
	})
	return updated, err
}

// SetWatching starts or stops reading the network. Stopping is refused while
// processing is enabled, outbound transactions await the chain or invoices
// await payment; transfers that arrive meanwhile are read from the watcher
// checkpoint when watching resumes.
func (s *NetworkPolicyAdminService) SetWatching(ctx context.Context, networkID id.ID, watching bool, reason string) (NetworkPolicyState, error) {
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return NetworkPolicyState{}, apperror.NewInternal(err)
	}
	var updated NetworkPolicyState
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		current, err := s.repo.GetNetworkPolicyState(txCtx, networkID)
		if err != nil {
			return err
		}
		if current.Watching == watching {
			updated = current
			return nil
		}
		updated, err = s.repo.SetNetworkWatching(txCtx, networkID, watching)
		if err != nil {
			return err
		}
		network := networkID
		if err := s.eventWriter.Write(txCtx, eventlog.Event{
			Category: eventlog.CategorySecurity, Severity: eventlog.SeverityWarning,
			EventType: eventlog.EventType("crypto.network.watching_changed"), Source: "api",
			EntityType: "blockchain_network", EntityID: &network, Message: "Crypto network watching changed",
			Details: map[string]any{"watching": watching, "reason": reason},
		}); err != nil {
			return fmt.Errorf("write crypto network watching audit event: %w", err)
		}
		return nil
	})
	if err != nil {
		return NetworkPolicyState{}, err
	}
	return s.decorate(updated)
}
