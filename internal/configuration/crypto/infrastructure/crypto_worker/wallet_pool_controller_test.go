package crypto_worker

import (
	"context"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
)

type controllerWalletDeriver struct{}

func (controllerWalletDeriver) DeriveAddress(context.Context, crypto.NetworkRef, string) (crypto.WalletDerivation, error) {
	return crypto.WalletDerivation{}, nil
}

type controllerNetworkDriver struct {
	network      crypto.NetworkRef
	capabilities crypto.ChainFamilyCapabilities
}

func (d controllerNetworkDriver) Network() crypto.NetworkRef { return d.network }
func (d controllerNetworkDriver) Capabilities() crypto.ChainFamilyCapabilities {
	return d.capabilities
}
func (d controllerNetworkDriver) Watchers() crypto.WatcherFactory             { return nil }
func (d controllerNetworkDriver) Transactions() crypto.TransactionBuilder     { return nil }
func (d controllerNetworkDriver) Broadcaster() crypto.Broadcaster             { return nil }
func (d controllerNetworkDriver) Finality() crypto.FinalityReader             { return nil }
func (d controllerNetworkDriver) Balances() crypto.BalanceReader              { return nil }
func (d controllerNetworkDriver) Wallets() crypto.WalletDeriver               { return controllerWalletDeriver{} }
func (d controllerNetworkDriver) Accounts() crypto.AccountLifecycle           { return nil }
func (d controllerNetworkDriver) TokenAccounts() crypto.TokenAccountLifecycle { return nil }
func (d controllerNetworkDriver) Addresses() crypto.AddressCodec              { return nil }

type walletPoolControllerRepoStub struct {
	state       crypto.WalletPoolState
	planCalls   int
	lastStatus  crypto.WalletPoolOperationalStatus
	planCreated int
}

func (r *walletPoolControllerRepoStub) ListStates(context.Context) ([]crypto.WalletPoolState, error) {
	return []crypto.WalletPoolState{r.state}, nil
}
func (r *walletPoolControllerRepoStub) GetState(context.Context, id.ID) (crypto.WalletPoolState, error) {
	return r.state, nil
}
func (r *walletPoolControllerRepoStub) ListRefillCandidates(context.Context) ([]id.ID, error) {
	return []id.ID{r.state.Policy.NetworkID}, nil
}
func (r *walletPoolControllerRepoStub) PlanNetworkRefill(context.Context, id.ID) (crypto.WalletPoolPlanResult, error) {
	r.planCalls++
	return crypto.WalletPoolPlanResult{NetworkID: r.state.Policy.NetworkID, Created: r.planCreated}, nil
}
func (r *walletPoolControllerRepoStub) ClaimProvisioningJobs(context.Context, string, int, time.Duration) ([]crypto.WalletProvisioningJob, error) {
	return nil, nil
}
func (r *walletPoolControllerRepoStub) CompleteProvisioningJob(context.Context, crypto.WalletProvisioningJob, crypto.WalletDerivation, crypto.WalletProvisioningMode) (id.ID, error) {
	return id.Nil(), nil
}
func (r *walletPoolControllerRepoStub) RetryProvisioningJob(context.Context, crypto.WalletProvisioningJob, error, time.Time, bool) error {
	return nil
}
func (r *walletPoolControllerRepoStub) ListPendingWalletReconciliations(context.Context, int) ([]crypto.WalletPendingReconciliation, error) {
	return nil, nil
}
func (r *walletPoolControllerRepoStub) SetOperationalStatus(_ context.Context, _ id.ID, status crypto.WalletPoolOperationalStatus) error {
	r.lastStatus = status
	return nil
}
func (r *walletPoolControllerRepoStub) RecordStateTransition(context.Context, crypto.WalletPoolState) error {
	return nil
}

func TestWalletPoolControllerValidatesPolicyAgainstDriverCapabilities(t *testing.T) {
	network := crypto.NetworkRef{
		ID:                    id.New(),
		Code:                  "ARBITRARY-DISPLAY-CODE",
		Family:                crypto.ChainFamilyEVM,
		NetworkIdentifier:     "31337",
		NativeTokenID:         id.New(),
		NativeDecimals:        18,
		RequiredConfirmations: 1,
		FinalityMode:          crypto.FinalityByConfirmations,
	}
	capabilities := crypto.ChainFamilyCapabilities{
		Family:               crypto.ChainFamilyEVM,
		WalletProvisioning:   crypto.WalletProvisioningDerived,
		RequiredRPCRoles:     []string{"rpc"},
		ExternalResourceMode: crypto.ExternalResourceNone,
		SequenceModel:        crypto.SequenceAccount,
		CheckpointTimeUnit:   crypto.CheckpointUnixSeconds,
		TokenAccountModel:    crypto.TokenAccountSameAddress,
	}
	drivers := crypto.NewChainDriverRegistry()
	if err := drivers.Register(controllerNetworkDriver{network: network, capabilities: capabilities}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	policy := crypto.WalletPoolPolicy{
		NetworkID:                          network.ID,
		NetworkCode:                        network.Code,
		ChainFamily:                        network.Family,
		Enabled:                            true,
		LowWatermark:                       1,
		TargetReady:                        2,
		MaxTotalWallets:                    3,
		MaxProvisionBatch:                  1,
		MaxActiveInvoiceLeasesPerMerchant:  1,
		MaxPersistentAddressesPerMerchant:  1,
		MaxLeaseStartsPerMinutePerMerchant: 1,
		MinGlobalReadyReserve:              0,
		RetryAfterSeconds:                  30,
		DerivationNamespace:                "m/44'/60'/0'/0",
		Version:                            1,
		Activation:                         &crypto.WalletPoolActivationPolicy{MaxConcurrent: 1},
	}
	repo := &walletPoolControllerRepoStub{state: crypto.WalletPoolState{Policy: policy}}
	controller := NewWalletPoolController(repo, drivers, nil, "test-worker", nil, nil)

	if _, err := controller.reconcileCapacity(context.Background()); err == nil {
		t.Fatal("reconcileCapacity() accepted a family-incompatible policy")
	}
	if repo.planCalls != 0 {
		t.Fatalf("PlanNetworkRefill() calls = %d, want 0", repo.planCalls)
	}
	if repo.lastStatus.ErrorCode != crypto.WalletPoolErrorPolicyInvalid {
		t.Fatalf("status = %q, want %q", repo.lastStatus.ErrorCode, crypto.WalletPoolErrorPolicyInvalid)
	}

	policy.Activation = nil
	repo.state.Policy = policy
	repo.lastStatus = crypto.WalletPoolOperationalStatus{}
	if _, err := controller.reconcileCapacity(context.Background()); err != nil {
		t.Fatalf("reconcileCapacity() error = %v", err)
	}
	if repo.planCalls != 1 {
		t.Fatalf("PlanNetworkRefill() calls = %d, want 1", repo.planCalls)
	}
}

func TestDependencyCircuitOpensAndUsesSingleHalfOpenProbe(t *testing.T) {
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	var circuit dependencyCircuit

	for attempt := 1; attempt < dependencyCircuitThreshold; attempt++ {
		circuit.failure(now)
		if !circuit.allow(now) {
			t.Fatalf("circuit opened after %d failures", attempt)
		}
	}
	circuit.failure(now)
	if circuit.allow(now.Add(dependencyCircuitOpenFor - time.Millisecond)) {
		t.Fatal("circuit allowed a request before the open interval elapsed")
	}
	if !circuit.allow(now.Add(dependencyCircuitOpenFor)) {
		t.Fatal("circuit did not allow its half-open probe")
	}
	if circuit.allow(now.Add(dependencyCircuitOpenFor)) {
		t.Fatal("circuit allowed more than one half-open probe")
	}

	circuit.cancelProbe()
	if !circuit.allow(now.Add(dependencyCircuitOpenFor)) {
		t.Fatal("cancelled half-open probe was not made available again")
	}
	circuit.success()
	if !circuit.allow(now) {
		t.Fatal("successful probe did not close the circuit")
	}
}

func TestProvisioningBackoffIsBounded(t *testing.T) {
	for _, attempt := range []int{1, 2, 5, 10, 100} {
		delay := provisioningBackoff(attempt)
		if delay < time.Second {
			t.Fatalf("attempt %d delay %s is below the minimum", attempt, delay)
		}
		if delay > 5*time.Minute+75*time.Second {
			t.Fatalf("attempt %d delay %s exceeds the bounded maximum", attempt, delay)
		}
	}
}
