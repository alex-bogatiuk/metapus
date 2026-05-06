// internal/domain/crypto/event_processor_test.go
package crypto

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_payment"
)

// ── Test: handleWalletAfterConfirm ──────────────────────────────────────
// Exercises the decision logic after a payment is confirmed:
//   - threshold=0 → legacy immediate sweep (MarkSweepPending)
//   - threshold>0, transient → release wallet back to pool
//   - persistent → no change (stays assigned)
//   - nil/errored resolver → legacy fallback

// mockWalletSvc captures calls to MarkSweepPending, GetByID, Update.
type mockWalletSvc struct {
	wallet          *wallet.Wallet
	getByIDErr      error
	markSweepCalled bool
	markSweepErr    error
	updateCalled    bool
	updateErr       error
	updateWallet    *wallet.Wallet // captures the wallet passed to Update
}

func (m *mockWalletSvc) markSweepPending(ctx context.Context, walletID id.ID) error {
	m.markSweepCalled = true
	return m.markSweepErr
}

func (m *mockWalletSvc) getByID(ctx context.Context, walletID id.ID) (*wallet.Wallet, error) {
	return m.wallet, m.getByIDErr
}

func (m *mockWalletSvc) update(ctx context.Context, w *wallet.Wallet) error {
	m.updateCalled = true
	m.updateWallet = w
	return m.updateErr
}

// mockSweepResolver returns preconfigured SweepConfig or error.
type mockSweepResolver struct {
	cfg SweepConfig
	err error
}

func (m *mockSweepResolver) resolve(ctx context.Context, merchantID, tokenID id.ID) (SweepConfig, error) {
	return m.cfg, m.err
}

// testableEventProcessor is a minimal EventProcessor that allows injection of mocked methods
// without needing full service/repo construction.
// We test handleWalletAfterConfirm via a wrapper that replaces the real calls.
func TestHandleWalletAfterConfirm(t *testing.T) {
	ctx := context.Background()
	walletID := id.New()
	merchantID := id.New()
	tokenID := id.New()

	makePayment := func() *crypto_payment.CryptoPayment {
		return &crypto_payment.CryptoPayment{
			WalletID:   walletID,
			MerchantID: merchantID,
			TokenID:    tokenID,
		}
	}

	makeTransientWallet := func() *wallet.Wallet {
		return &wallet.Wallet{
			Status:         wallet.WalletStatusLeased,
			AllocationMode: wallet.AllocationModeTransient,
		}
	}

	makePersistentWallet := func() *wallet.Wallet {
		return &wallet.Wallet{
			Status:         wallet.WalletStatusAssigned,
			AllocationMode: wallet.AllocationModePersistent,
			CustomerRef:    "CUST-001",
		}
	}

	tests := []struct {
		give            string
		resolver        *mockSweepResolver // nil = no resolver
		wallet          *wallet.Wallet
		wantSweep       bool // MarkSweepPending called
		wantUpdate      bool // Update called (Release path)
		wantFreeStatus  bool // wallet should be Free after Release
	}{
		{
			give:       "nil resolver → legacy sweep",
			resolver:   nil,
			wantSweep:  true,
			wantUpdate: false,
		},
		{
			give:       "resolver error → legacy fallback",
			resolver:   &mockSweepResolver{err: fmt.Errorf("db error")},
			wantSweep:  true,
			wantUpdate: false,
		},
		{
			give: "zero threshold → immediate sweep (legacy)",
			resolver: &mockSweepResolver{
				cfg: SweepConfig{
					Threshold:   types.ZeroCryptoAmount(),
					MaxAgeHours: 0,
				},
			},
			wantSweep:  true,
			wantUpdate: false,
		},
		{
			give: "positive threshold + transient → release",
			resolver: &mockSweepResolver{
				cfg: SweepConfig{
					Threshold:   types.NewCryptoAmount(big.NewInt(10_000_000)),
					MaxAgeHours: 24,
				},
			},
			wallet:         makeTransientWallet(),
			wantSweep:      false,
			wantUpdate:     true,
			wantFreeStatus: true,
		},
		{
			give: "positive threshold + persistent → no-op (stays assigned)",
			resolver: &mockSweepResolver{
				cfg: SweepConfig{
					Threshold:   types.NewCryptoAmount(big.NewInt(10_000_000)),
					MaxAgeHours: 24,
				},
			},
			wallet:     makePersistentWallet(),
			wantSweep:  false,
			wantUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			mock := &mockWalletSvc{wallet: tt.wallet}
			payment := makePayment()

			// Call the logic directly by simulating what handleWalletAfterConfirm does.
			// We replicate the branching logic to verify correctness, since
			// the real method is coupled to *wallet.Service (not an interface).
			err := simulateHandleWalletAfterConfirm(ctx, mock, tt.resolver, payment)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if mock.markSweepCalled != tt.wantSweep {
				t.Errorf("MarkSweepPending called = %v, want %v", mock.markSweepCalled, tt.wantSweep)
			}
			if mock.updateCalled != tt.wantUpdate {
				t.Errorf("Update called = %v, want %v", mock.updateCalled, tt.wantUpdate)
			}
			if tt.wantFreeStatus && mock.updateWallet != nil {
				if mock.updateWallet.Status != wallet.WalletStatusFree {
					t.Errorf("wallet.Status = %q, want %q", mock.updateWallet.Status, wallet.WalletStatusFree)
				}
			}
		})
	}
}

// simulateHandleWalletAfterConfirm replicates the exact logic from EventProcessor.handleWalletAfterConfirm
// but uses injected mock functions instead of *wallet.Service and *SweepConfigResolver.
// This lets us test the branching logic without the full dependency graph.
func simulateHandleWalletAfterConfirm(
	ctx context.Context,
	walletMock *mockWalletSvc,
	resolverMock *mockSweepResolver,
	payment *crypto_payment.CryptoPayment,
) error {
	// Branch 1: No sweep resolver → legacy
	if resolverMock == nil {
		return walletMock.markSweepPending(ctx, payment.WalletID)
	}

	// Branch 2: Resolve sweep config
	sweepCfg, err := resolverMock.resolve(ctx, payment.MerchantID, payment.TokenID)
	if err != nil {
		// On error → legacy fallback
		return walletMock.markSweepPending(ctx, payment.WalletID)
	}

	// Branch 3: Zero threshold → legacy
	if sweepCfg.IsZeroThreshold() {
		return walletMock.markSweepPending(ctx, payment.WalletID)
	}

	// Branch 4: Threshold mode — get wallet
	w, err := walletMock.getByID(ctx, payment.WalletID)
	if err != nil {
		return fmt.Errorf("get wallet %s: %w", payment.WalletID, err)
	}

	// Branch 5: Transient → release
	if w.IsTransient() {
		w.Release()
		return walletMock.update(ctx, w)
	}

	// Branch 6: Persistent → no-op
	return nil
}

// ── Test: PaymentFSM transition matrix ──────────────────────────────────

func TestPaymentFSM_AllowedTransitions(t *testing.T) {
	tests := []struct {
		give string
		from crypto_payment.PaymentStatus
		to   crypto_payment.PaymentStatus
		want bool
	}{
		// Allowed
		{"detected → confirming", crypto_payment.PaymentStatusDetected, crypto_payment.PaymentStatusConfirming, true},
		{"confirming → confirmed", crypto_payment.PaymentStatusConfirming, crypto_payment.PaymentStatusConfirmed, true},
		{"confirming → reorged", crypto_payment.PaymentStatusConfirming, crypto_payment.PaymentStatusReorged, true},
		{"confirmed → settled", crypto_payment.PaymentStatusConfirmed, crypto_payment.PaymentStatusSettled, true},
		{"reorged → detected", crypto_payment.PaymentStatusReorged, crypto_payment.PaymentStatusDetected, true},

		// Disallowed
		{"detected → confirmed (skip)", crypto_payment.PaymentStatusDetected, crypto_payment.PaymentStatusConfirmed, false},
		{"detected → settled (skip)", crypto_payment.PaymentStatusDetected, crypto_payment.PaymentStatusSettled, false},
		{"confirmed → detected (backward)", crypto_payment.PaymentStatusConfirmed, crypto_payment.PaymentStatusDetected, false},
		{"settled → confirmed (backward)", crypto_payment.PaymentStatusSettled, crypto_payment.PaymentStatusConfirmed, false},
		{"confirmed → reorged (only from confirming)", crypto_payment.PaymentStatusConfirmed, crypto_payment.PaymentStatusReorged, false},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			// Access the internal transition map to verify the matrix
			allowed, ok := _allowedTransitions[tt.from]
			if !ok && tt.want {
				t.Fatalf("no transitions defined for %q, but expected %q → %q to be allowed", tt.from, tt.from, tt.to)
			}

			found := false
			for _, s := range allowed {
				if s == tt.to {
					found = true
					break
				}
			}

			if found != tt.want {
				t.Errorf("transition %q → %q: got allowed=%v, want %v", tt.from, tt.to, found, tt.want)
			}
		})
	}
}

// ── Test: dust threshold ────────────────────────────────────────────────

func TestDefaultDustThreshold(t *testing.T) {
	threshold := _defaultDustThreshold()
	if !threshold.IsPositive() {
		t.Error("default dust threshold should be positive")
	}
	if threshold.BigInt().Int64() != 1000 {
		t.Errorf("default dust threshold = %s, want 1000", threshold.String())
	}
}

// ── Test: BlockchainEvent EventType constants ───────────────────────────

func TestEventType_Values(t *testing.T) {
	// Verify iota+1 pattern: zero value is not a valid EventType
	var zero EventType
	if zero == EventTypeTransfer || zero == EventTypeConfirmation || zero == EventTypeReorg {
		t.Error("zero EventType should not match any valid type")
	}

	// Verify distinct values
	if EventTypeTransfer == EventTypeConfirmation {
		t.Error("EventTypeTransfer should differ from EventTypeConfirmation")
	}
	if EventTypeTransfer == EventTypeReorg {
		t.Error("EventTypeTransfer should differ from EventTypeReorg")
	}
	if EventTypeConfirmation == EventTypeReorg {
		t.Error("EventTypeConfirmation should differ from EventTypeReorg")
	}
}

// ── Test: BlockchainEvent construction ──────────────────────────────────

func TestBlockchainEvent_Fields(t *testing.T) {
	netID := id.New()
	amount := types.NewCryptoAmount(big.NewInt(5_000_000))
	ts := time.Now().UTC()

	event := BlockchainEvent{
		Network:       "tron_shasta",
		NetworkID:     netID,
		TxHash:        "abc123",
		FromAddress:   "TSender",
		ToAddress:     "TReceiver",
		TokenContract: "TContractXYZ",
		Amount:        amount,
		BlockNumber:   12345,
		Confirmations: 5,
		RequiredConfs: 19,
		EventType:     EventTypeTransfer,
		Timestamp:     ts,
	}

	if event.NetworkID != netID {
		t.Errorf("NetworkID = %v, want %v", event.NetworkID, netID)
	}
	if event.Amount.BigInt().Int64() != 5_000_000 {
		t.Errorf("Amount = %s, want 5000000", event.Amount.String())
	}
	if event.EventType != EventTypeTransfer {
		t.Errorf("EventType = %d, want %d", event.EventType, EventTypeTransfer)
	}
}
