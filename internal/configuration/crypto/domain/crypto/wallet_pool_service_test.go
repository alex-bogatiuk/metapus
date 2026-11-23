package crypto

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/appctx"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/eventlog"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

type walletPoolServiceTxManager struct{}

func (walletPoolServiceTxManager) RunInTransaction(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	return fn(ctx)
}

type walletPoolAdminRepoStub struct {
	state         WalletPoolState
	funded        bool
	fundingChecks int
	updateCalls   int
	updatedBy     string
	plans         []WalletPoolPlanResult
}

type walletPoolCapabilitiesResolver map[ChainFamily]ChainFamilyCapabilities

func (r walletPoolCapabilitiesResolver) ResolveCapabilities(family ChainFamily) (ChainFamilyCapabilities, error) {
	capabilities, ok := r[family]
	if !ok {
		return ChainFamilyCapabilities{}, ErrChainFamilyCapabilitiesNotFound
	}
	return capabilities, nil
}

func newWalletPoolAdminService(
	repo WalletPoolAdminRepository,
	writer eventlog.Writer,
) *WalletPoolAdminService {
	return NewWalletPoolAdminService(repo, walletPoolCapabilitiesResolver{
		ChainFamilyTRON: tronWalletPoolCapabilities(),
	}, writer)
}

func (r *walletPoolAdminRepoStub) ListStates(context.Context) ([]WalletPoolState, error) {
	return []WalletPoolState{r.state}, nil
}

func (r *walletPoolAdminRepoStub) GetState(context.Context, id.ID) (WalletPoolState, error) {
	return r.state, nil
}

func (r *walletPoolAdminRepoStub) UpdatePolicy(
	_ context.Context,
	policy WalletPoolPolicy,
	updatedBy string,
) (WalletPoolState, error) {
	r.updateCalls++
	r.updatedBy = updatedBy
	policy.Version++
	r.state.Policy = policy
	return r.state, nil
}

func (r *walletPoolAdminRepoStub) HasConfirmedNativeFunding(context.Context, id.ID) (bool, error) {
	r.fundingChecks++
	return r.funded, nil
}

func (r *walletPoolAdminRepoStub) PlanNetworkRefill(
	_ context.Context,
	networkID id.ID,
) (WalletPoolPlanResult, error) {
	if len(r.plans) == 0 {
		return WalletPoolPlanResult{NetworkID: networkID}, nil
	}
	return r.plans[0], nil
}

func (r *walletPoolAdminRepoStub) GetMerchantAllocationOverride(
	context.Context,
	id.ID,
	id.ID,
) (MerchantWalletAllocationOverride, bool, error) {
	return MerchantWalletAllocationOverride{}, false, nil
}

func (r *walletPoolAdminRepoStub) UpsertMerchantAllocationOverride(
	_ context.Context,
	override MerchantWalletAllocationOverride,
	_ string,
) (MerchantWalletAllocationOverride, error) {
	override.Version++
	return override, nil
}

type walletPoolEventWriterStub struct {
	events []eventlog.Event
	err    error
}

func (w *walletPoolEventWriterStub) Write(_ context.Context, event eventlog.Event) error {
	w.events = append(w.events, event)
	return w.err
}

func (w *walletPoolEventWriterStub) WriteBatch(_ context.Context, events []eventlog.Event) error {
	w.events = append(w.events, events...)
	return w.err
}

func TestWalletPoolAdminServiceUpdatePolicyOwnsOrchestration(t *testing.T) {
	networkID := id.New()
	policy := validWalletPoolPolicy()
	policy.NetworkID = networkID
	policy.NetworkCode = "CUSTOM-TRON-TESTNET"
	policy.NetworkName = "Nile"
	repo := &walletPoolAdminRepoStub{
		state:  WalletPoolState{Policy: policy},
		funded: true,
	}
	writer := &walletPoolEventWriterStub{}
	svc := newWalletPoolAdminService(repo, writer)

	ctx := tenant.WithTxManager(context.Background(), walletPoolServiceTxManager{})
	ctx = appctx.WithUser(ctx, &appctx.UserContext{
		UserID: "01900000-0000-7000-8000-000000000001",
		Email:  "admin@example.test",
	})
	target := 7
	updated, err := svc.UpdatePolicy(ctx, networkID, WalletPoolPolicyPatch{
		TargetReady: &target,
		Version:     policy.Version,
	})
	if err != nil {
		t.Fatalf("UpdatePolicy() error = %v", err)
	}
	if updated.Policy.TargetReady != target {
		t.Fatalf("TargetReady = %d, want %d", updated.Policy.TargetReady, target)
	}
	if repo.fundingChecks != 1 || repo.updateCalls != 1 {
		t.Fatalf("fundingChecks=%d updateCalls=%d", repo.fundingChecks, repo.updateCalls)
	}
	if repo.updatedBy != "admin@example.test" {
		t.Fatalf("updatedBy = %q", repo.updatedBy)
	}
	if len(writer.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(writer.events))
	}
}

func TestWalletPoolAdminServiceRejectsEnablementWithoutFunding(t *testing.T) {
	networkID := id.New()
	policy := validWalletPoolPolicy()
	policy.NetworkID = networkID
	policy.NetworkCode = "TRON-MAINNET"
	policy.Enabled = false
	repo := &walletPoolAdminRepoStub{state: WalletPoolState{Policy: policy}}
	svc := newWalletPoolAdminService(repo, &walletPoolEventWriterStub{})
	ctx := tenant.WithTxManager(context.Background(), walletPoolServiceTxManager{})
	enabled := true

	_, err := svc.UpdatePolicy(ctx, networkID, WalletPoolPolicyPatch{
		Enabled: &enabled,
		Version: policy.Version,
	})
	if err == nil {
		t.Fatal("UpdatePolicy() returned nil")
	}
	if repo.updateCalls != 0 {
		t.Fatalf("updateCalls = %d, want 0", repo.updateCalls)
	}
}

func TestWalletPoolAdminServiceRejectsUnsupportedFamilyWithoutInspectingCode(t *testing.T) {
	networkID := id.New()
	policy := validWalletPoolPolicy()
	policy.NetworkID = networkID
	policy.NetworkCode = "CUSTOM-EVM-NAME"
	policy.ChainFamily = ChainFamilyEVM
	policy.Enabled = false
	repo := &walletPoolAdminRepoStub{state: WalletPoolState{Policy: policy}, funded: true}
	svc := newWalletPoolAdminService(repo, &walletPoolEventWriterStub{})
	ctx := tenant.WithTxManager(context.Background(), walletPoolServiceTxManager{})
	enabled := true

	_, err := svc.UpdatePolicy(ctx, networkID, WalletPoolPolicyPatch{Enabled: &enabled, Version: policy.Version})
	if err == nil {
		t.Fatal("UpdatePolicy() returned nil")
	}
	if repo.fundingChecks != 0 || repo.updateCalls != 0 {
		t.Fatalf("fundingChecks=%d updateCalls=%d, want 0", repo.fundingChecks, repo.updateCalls)
	}
}

func TestWalletPoolAdminServicePropagatesAuditFailure(t *testing.T) {
	networkID := id.New()
	policy := validWalletPoolPolicy()
	policy.NetworkID = networkID
	policy.NetworkCode = "TRON-NILE"
	repo := &walletPoolAdminRepoStub{
		state:  WalletPoolState{Policy: policy},
		funded: true,
	}
	writer := &walletPoolEventWriterStub{err: errors.New("audit unavailable")}
	svc := newWalletPoolAdminService(repo, writer)
	ctx := tenant.WithTxManager(context.Background(), walletPoolServiceTxManager{})

	_, err := svc.UpdatePolicy(ctx, networkID, WalletPoolPolicyPatch{Version: policy.Version})
	if err == nil {
		t.Fatal("UpdatePolicy() returned nil")
	}
}

func TestWalletPoolAdminServiceReconcileReturnsPlannedState(t *testing.T) {
	networkID := id.New()
	policy := validWalletPoolPolicy()
	policy.NetworkID = networkID
	now := time.Now().UTC()
	policy.UpdatedAt = now
	repo := &walletPoolAdminRepoStub{
		state: WalletPoolState{Policy: policy},
		plans: []WalletPoolPlanResult{{NetworkID: networkID, Created: 3}},
	}
	svc := newWalletPoolAdminService(repo, &walletPoolEventWriterStub{})

	plans, state, err := svc.Reconcile(context.Background(), networkID)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(plans) != 1 || plans[0].Created != 3 {
		t.Fatalf("plans = %#v", plans)
	}
	if state.Policy.NetworkID != networkID {
		t.Fatalf("state network = %s", state.Policy.NetworkID)
	}
}

func TestWalletPoolAdminServiceEnablesDerivedFamilyWithoutActivationFunding(t *testing.T) {
	networkID := id.New()
	policy := validWalletPoolPolicy()
	policy.NetworkID = networkID
	policy.NetworkCode = "ARBITRARY-EVM-NETWORK"
	policy.ChainFamily = ChainFamilyEVM
	policy.Enabled = false
	policy.Activation = nil
	repo := &walletPoolAdminRepoStub{state: WalletPoolState{Policy: policy}}
	svc := NewWalletPoolAdminService(repo, walletPoolCapabilitiesResolver{
		ChainFamilyEVM: evmWalletPoolCapabilities(),
	}, &walletPoolEventWriterStub{})
	ctx := tenant.WithTxManager(context.Background(), walletPoolServiceTxManager{})
	enabled := true

	_, err := svc.UpdatePolicy(ctx, networkID, WalletPoolPolicyPatch{
		Enabled: &enabled,
		Version: policy.Version,
	})
	if err != nil {
		t.Fatalf("UpdatePolicy() error = %v", err)
	}
	if repo.fundingChecks != 0 || repo.updateCalls != 1 {
		t.Fatalf("fundingChecks=%d updateCalls=%d, want 0 and 1", repo.fundingChecks, repo.updateCalls)
	}
}
