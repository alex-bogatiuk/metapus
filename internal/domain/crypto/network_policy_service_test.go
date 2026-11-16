package crypto

import (
	"context"
	"testing"
	"time"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
)

type networkPolicyAdminRepoStub struct {
	state           NetworkPolicyState
	feeWalletReady  bool
	feeWalletChecks int
	updateCalls     int
	updatedBy       string
	contractAssets  bool
	accountCodes    map[id.ID]string
	watchingCalls   int
}

func (r *networkPolicyAdminRepoStub) SetNetworkWatching(_ context.Context, _ id.ID, watching bool) (NetworkPolicyState, error) {
	r.watchingCalls++
	r.state.Watching = watching
	return r.state, nil
}

func (r *networkPolicyAdminRepoStub) NetworkHasActiveContractAssets(context.Context, id.ID) (bool, error) {
	return r.contractAssets, nil
}

func (r *networkPolicyAdminRepoStub) NetworkProviderAccountCodes(context.Context, id.ID, []id.ID, time.Time) (map[id.ID]string, error) {
	return r.accountCodes, nil
}

func (r *networkPolicyAdminRepoStub) ListNetworkPolicyStates(context.Context) ([]NetworkPolicyState, error) {
	return []NetworkPolicyState{r.state}, nil
}

func (r *networkPolicyAdminRepoStub) GetNetworkPolicyState(context.Context, id.ID) (NetworkPolicyState, error) {
	return r.state, nil
}

func (r *networkPolicyAdminRepoStub) NetworkFundingSourcesReady(context.Context, id.ID, types.RawAmount) (bool, error) {
	r.feeWalletChecks++
	return r.feeWalletReady, nil
}

func (r *networkPolicyAdminRepoStub) UpdateNetworkPolicy(_ context.Context, policy NetworkPolicy, _ int, actor string) (NetworkPolicyState, error) {
	r.updateCalls++
	r.updatedBy = actor
	policy.Version++
	r.state.Policy = policy
	return r.state, nil
}

func TestNetworkPolicyAdminServiceEnablementGateAndAudit(t *testing.T) {
	now := time.Now().UTC()
	state := validNetworkPolicyState(now)
	state.Policy.Enabled = false
	repo := &networkPolicyAdminRepoStub{state: state, feeWalletReady: true, accountCodes: map[id.ID]string{state.Policy.ExternalResources.ProviderAccountIDs[0]: "ITRX-TEST"}}
	writer := &walletPoolEventWriterStub{}
	svc := NewNetworkPolicyAdminService(repo, walletPoolCapabilitiesResolver{ChainFamilyTRON: tronWalletPoolCapabilities()}, writer)
	svc.now = func() time.Time { return now }
	ctx := networkPolicyTestContext()
	enabled := true

	updated, err := svc.Update(ctx, state.Network.ID, NetworkPolicyPatch{Version: state.Policy.Version, Enabled: &enabled})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !updated.Policy.Enabled || repo.feeWalletChecks != 1 || repo.updateCalls != 1 {
		t.Fatalf("enabled=%v checks=%d updates=%d", updated.Policy.Enabled, repo.feeWalletChecks, repo.updateCalls)
	}
	if repo.updatedBy != "admin@example.test" {
		t.Fatalf("updatedBy = %q", repo.updatedBy)
	}
	if len(writer.events) != 1 || writer.events[0].EventType != "crypto.network.policy_updated" {
		t.Fatalf("audit events = %#v", writer.events)
	}
}

func TestNetworkPolicyAdminServiceRejectsStaleRuntime(t *testing.T) {
	now := time.Now().UTC()
	state := validNetworkPolicyState(now)
	state.Policy.Enabled = false
	state.Runtime.ObservedAt = now.Add(-2 * time.Minute)
	repo := &networkPolicyAdminRepoStub{state: state, feeWalletReady: true}
	svc := NewNetworkPolicyAdminService(repo, walletPoolCapabilitiesResolver{ChainFamilyTRON: tronWalletPoolCapabilities()}, &walletPoolEventWriterStub{})
	svc.now = func() time.Time { return now }
	enabled := true

	_, err := svc.Update(networkPolicyTestContext(), state.Network.ID, NetworkPolicyPatch{Version: state.Policy.Version, Enabled: &enabled})
	if err == nil {
		t.Fatal("Update() returned nil for stale runtime status")
	}
	if repo.updateCalls != 0 {
		t.Fatalf("updateCalls = %d, want 0", repo.updateCalls)
	}
}

func TestNetworkPolicyAdminServiceRejectsStaleVersion(t *testing.T) {
	state := validNetworkPolicyState(time.Now().UTC())
	repo := &networkPolicyAdminRepoStub{state: state, feeWalletReady: true}
	svc := NewNetworkPolicyAdminService(repo, walletPoolCapabilitiesResolver{ChainFamilyTRON: tronWalletPoolCapabilities()}, &walletPoolEventWriterStub{})

	_, err := svc.Update(networkPolicyTestContext(), state.Network.ID, NetworkPolicyPatch{Version: state.Policy.Version + 1})
	if err == nil {
		t.Fatal("Update() returned nil for stale policy version")
	}
	if repo.updateCalls != 0 {
		t.Fatalf("updateCalls = %d, want 0", repo.updateCalls)
	}
}

func TestNetworkPolicyAdminServiceBurnOnlyDoesNotRequireResourceProvider(t *testing.T) {
	now := time.Now().UTC()
	state := validNetworkPolicyState(now)
	state.Policy.Enabled = false
	state.Policy.ExternalResources.Strategy = ResourceBurnOnly
	state.Runtime.ResourceProviderReady = false
	repo := &networkPolicyAdminRepoStub{state: state, feeWalletReady: true}
	svc := NewNetworkPolicyAdminService(repo, walletPoolCapabilitiesResolver{ChainFamilyTRON: tronWalletPoolCapabilities()}, &walletPoolEventWriterStub{})
	svc.now = func() time.Time { return now }
	enabled := true

	if _, err := svc.Update(networkPolicyTestContext(), state.Network.ID, NetworkPolicyPatch{Version: state.Policy.Version, Enabled: &enabled}); err != nil {
		t.Fatalf("Update() burn-only error = %v", err)
	}
}

func TestNetworkPolicyAdminServiceEnablesNewAccountFromBurnOnly(t *testing.T) {
	now := time.Now().UTC()
	state := validNetworkPolicyState(now)
	state.Policy.Enabled = false
	state.Policy.ExternalResources.Strategy = ResourceBurnOnly
	state.Runtime.ResourceProviderReady = false
	accountID := state.Policy.ExternalResources.ProviderAccountIDs[0]
	repo := &networkPolicyAdminRepoStub{state: state, feeWalletReady: true,
		accountCodes: map[id.ID]string{accountID: "ITRX-TEST"}}
	svc := NewNetworkPolicyAdminService(repo, walletPoolCapabilitiesResolver{ChainFamilyTRON: tronWalletPoolCapabilities()}, &walletPoolEventWriterStub{})
	svc.now = func() time.Time { return now }
	enabled := true
	resources := *state.Policy.ExternalResources
	resources.Strategy = ResourceRentalRequired
	_, err := svc.Update(networkPolicyTestContext(), state.Network.ID, NetworkPolicyPatch{
		Version: state.Policy.Version, Enabled: &enabled, ExternalResources: &resources,
	})
	if err != nil {
		t.Fatalf("enable new account from burn_only: %v", err)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", repo.updateCalls)
	}
}

func TestNetworkPolicyAdminServiceRejectsUnreadySelectedAccount(t *testing.T) {
	now := time.Now().UTC()
	state := validNetworkPolicyState(now)
	state.Policy.Enabled = false
	state.Runtime.ResourceProviders[0].State = "open"
	accountID := state.Policy.ExternalResources.ProviderAccountIDs[0]
	repo := &networkPolicyAdminRepoStub{state: state, feeWalletReady: true,
		accountCodes: map[id.ID]string{accountID: "ITRX-TEST"}}
	svc := NewNetworkPolicyAdminService(repo, walletPoolCapabilitiesResolver{ChainFamilyTRON: tronWalletPoolCapabilities()}, &walletPoolEventWriterStub{})
	svc.now = func() time.Time { return now }
	enabled := true
	if _, err := svc.Update(networkPolicyTestContext(), state.Network.ID, NetworkPolicyPatch{Version: state.Policy.Version, Enabled: &enabled}); err == nil {
		t.Fatal("enabled network with an open selected account")
	}
}

func TestNetworkPolicyAdminServiceRejectsMissingSelectedAccount(t *testing.T) {
	now := time.Now().UTC()
	state := validNetworkPolicyState(now)
	state.Policy.Enabled = false
	repo := &networkPolicyAdminRepoStub{state: state, feeWalletReady: true}
	svc := NewNetworkPolicyAdminService(repo, walletPoolCapabilitiesResolver{ChainFamilyTRON: tronWalletPoolCapabilities()}, &walletPoolEventWriterStub{})
	svc.now = func() time.Time { return now }
	enabled := true
	if _, err := svc.Update(networkPolicyTestContext(), state.Network.ID, NetworkPolicyPatch{Version: state.Policy.Version, Enabled: &enabled}); err == nil {
		t.Fatal("enabled network with a missing selected account")
	}
	if repo.updateCalls != 0 {
		t.Fatalf("updateCalls = %d, want 0", repo.updateCalls)
	}
}

func validNetworkPolicyState(now time.Time) NetworkPolicyState {
	networkID := id.New()
	return NetworkPolicyState{
		Network:  NetworkRef{ID: networkID, Code: "TRON-NILE", Family: ChainFamilyTRON, NetworkIdentifier: "nile", NativeTokenID: id.New(), NativeDecimals: 6, RequiredConfirmations: 20},
		Watching: true,
		Policy: NetworkPolicy{
			NetworkID: networkID, Enabled: true,
			PerTransactionMaxFee: types.NewRawAmountFromInt64(100), DailyNetworkFeeBudget: types.NewRawAmountFromInt64(1_000),
			FeeFunding: FeeFundingPolicy{Mode: FeeFundingJIT, DailyBudget: types.NewRawAmountFromInt64(2_000),
				MinFeeWalletBalance: types.NewRawAmountFromInt64(10), MinSourceWalletResidual: types.NewRawAmountFromInt64(1), MaxFundingAmount: types.NewRawAmountFromInt64(101)},
			ExternalResources: &ExternalResourcePolicy{Strategy: ResourcePreferRental, ProviderAccountIDs: []id.ID{id.New()}, ProviderFailureThreshold: 3, ProviderOpenDuration: time.Minute},
			RetryDelay:        time.Second, RPCFailureThreshold: 3, RPCOpenDuration: time.Minute,
			Version: 1,
		},
		Runtime: NetworkRuntimeStatus{
			NetworkID: networkID, SignerReady: true, ResourceProviderReady: true,
			ResourceProviderState: "closed", ObservedAt: now,
			ResourceProviders: []RuntimeEndpointStatus{{ProviderID: "ITRX-TEST", Role: "resource", State: "closed"}},
			RPCEndpoints: []RuntimeEndpointStatus{
				{ProviderID: "indexer-a", Role: "indexer", State: "closed"},
				{ProviderID: "fullnode-a", Role: "fullnode", State: "closed"},
				{ProviderID: "solidity-a", Role: "solidity", State: "closed"},
			},
		},
	}
}

func networkPolicyTestContext() context.Context {
	ctx := tenant.WithTxManager(context.Background(), walletPoolServiceTxManager{})
	return appctx.WithUser(ctx, &appctx.UserContext{UserID: id.New().String(), Email: "admin@example.test"})
}

func TestNetworkPolicyAdminServiceRequiresWatchingToEnableProcessing(t *testing.T) {
	now := time.Now().UTC()
	state := validNetworkPolicyState(now)
	state.Policy.Enabled, state.Watching = false, false
	repo := &networkPolicyAdminRepoStub{state: state, feeWalletReady: true, accountCodes: map[id.ID]string{state.Policy.ExternalResources.ProviderAccountIDs[0]: "ITRX-TEST"}}
	svc := NewNetworkPolicyAdminService(repo, walletPoolCapabilitiesResolver{ChainFamilyTRON: tronWalletPoolCapabilities()}, &walletPoolEventWriterStub{})
	svc.now = func() time.Time { return now }
	enabled := true

	if _, err := svc.Update(networkPolicyTestContext(), state.Network.ID, NetworkPolicyPatch{Version: state.Policy.Version, Enabled: &enabled}); err == nil {
		t.Fatal("processing of an unwatched network was enabled")
	}
	if repo.updateCalls != 0 {
		t.Fatalf("updates = %d", repo.updateCalls)
	}
}

func TestNetworkPolicyAdminServiceAuditsOnlyActualWatchingChanges(t *testing.T) {
	state := validNetworkPolicyState(time.Now().UTC())
	state.Policy.Enabled = false
	repo := &networkPolicyAdminRepoStub{state: state}
	writer := &walletPoolEventWriterStub{}
	svc := NewNetworkPolicyAdminService(repo, walletPoolCapabilitiesResolver{ChainFamilyTRON: tronWalletPoolCapabilities()}, writer)
	ctx := networkPolicyTestContext()

	updated, err := svc.SetWatching(ctx, state.Network.ID, false, "stand setup")
	if err != nil {
		t.Fatalf("SetWatching() error = %v", err)
	}
	if updated.Watching || repo.watchingCalls != 1 || len(writer.events) != 1 || writer.events[0].EventType != "crypto.network.watching_changed" {
		t.Fatalf("watching=%v calls=%d events=%#v", updated.Watching, repo.watchingCalls, writer.events)
	}
	if _, err := svc.SetWatching(ctx, state.Network.ID, false, "repeat"); err != nil {
		t.Fatalf("repeated SetWatching() error = %v", err)
	}
	if repo.watchingCalls != 1 || len(writer.events) != 1 {
		t.Fatalf("a repeated command changed state: calls=%d events=%d", repo.watchingCalls, len(writer.events))
	}
}
