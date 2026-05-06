// internal/domain/catalogs/wallet/model_test.go
package wallet

import (
	"context"
	"testing"
	"time"

	"metapus/internal/core/id"
)

func TestWallet_IsTransient(t *testing.T) {
	tests := []struct {
		give string
		mode AllocationMode
		want bool
	}{
		{"transient → true", AllocationModeTransient, true},
		{"persistent → false", AllocationModePersistent, false},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			w := &Wallet{AllocationMode: tt.mode}
			if got := w.IsTransient(); got != tt.want {
				t.Errorf("IsTransient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWallet_IsPersistent(t *testing.T) {
	tests := []struct {
		give string
		mode AllocationMode
		want bool
	}{
		{"persistent → true", AllocationModePersistent, true},
		{"transient → false", AllocationModeTransient, false},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			w := &Wallet{AllocationMode: tt.mode}
			if got := w.IsPersistent(); got != tt.want {
				t.Errorf("IsPersistent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWallet_Release(t *testing.T) {
	invID := id.New()
	until := time.Now().Add(30 * time.Minute)

	w := &Wallet{
		Status:      WalletStatusLeased,
		LeasedForID: &invID,
		LeasedUntil: &until,
	}

	w.Release()

	if w.Status != WalletStatusFree {
		t.Errorf("Status = %q, want %q", w.Status, WalletStatusFree)
	}
	if w.LeasedForID != nil {
		t.Errorf("LeasedForID = %v, want nil", w.LeasedForID)
	}
	if w.LeasedUntil != nil {
		t.Errorf("LeasedUntil = %v, want nil", w.LeasedUntil)
	}
}

func TestWallet_MarkSweepPending(t *testing.T) {
	invID := id.New()
	until := time.Now().Add(30 * time.Minute)

	w := &Wallet{
		Status:      WalletStatusLeased,
		LeasedForID: &invID,
		LeasedUntil: &until,
	}

	w.MarkSweepPending()

	if w.Status != WalletStatusSweepPending {
		t.Errorf("Status = %q, want %q", w.Status, WalletStatusSweepPending)
	}
	if w.LeasedForID != nil {
		t.Errorf("LeasedForID = %v, want nil", w.LeasedForID)
	}
	if w.LeasedUntil != nil {
		t.Errorf("LeasedUntil = %v, want nil", w.LeasedUntil)
	}
}

func TestWallet_Lease(t *testing.T) {
	w := &Wallet{Status: WalletStatusFree}
	invoiceID := id.New()
	until := time.Now().Add(30 * time.Minute)

	w.Lease(invoiceID, until)

	if w.Status != WalletStatusLeased {
		t.Errorf("Status = %q, want %q", w.Status, WalletStatusLeased)
	}
	if w.LeasedForID == nil || *w.LeasedForID != invoiceID {
		t.Errorf("LeasedForID = %v, want %v", w.LeasedForID, invoiceID)
	}
	if w.LeasedUntil == nil || !w.LeasedUntil.Equal(until) {
		t.Errorf("LeasedUntil = %v, want %v", w.LeasedUntil, until)
	}
}

func TestWallet_Validate(t *testing.T) {
	ctx := context.Background()

	// Helper to create a valid base wallet
	validWallet := func() *Wallet {
		return NewWallet("W-001", "Pool Wallet 1", id.New(), "TAddr123", "m/44'/195'/0'/0/0")
	}

	tests := []struct {
		give    string
		modify  func(w *Wallet)
		wantErr bool
	}{
		{
			give:    "valid transient wallet",
			modify:  func(w *Wallet) {},
			wantErr: false,
		},
		{
			give: "valid persistent wallet with CustomerRef",
			modify: func(w *Wallet) {
				w.AllocationMode = AllocationModePersistent
				w.CustomerRef = "CUST-001"
			},
			wantErr: false,
		},
		{
			give: "persistent without CustomerRef → error",
			modify: func(w *Wallet) {
				w.AllocationMode = AllocationModePersistent
				w.CustomerRef = ""
			},
			wantErr: true,
		},
		{
			give: "transient without CustomerRef → OK",
			modify: func(w *Wallet) {
				w.AllocationMode = AllocationModeTransient
				w.CustomerRef = ""
			},
			wantErr: false,
		},
		{
			give: "invalid AllocationMode → error",
			modify: func(w *Wallet) {
				w.AllocationMode = "invalid"
			},
			wantErr: true,
		},
		{
			give: "empty address → error",
			modify: func(w *Wallet) {
				w.Address = ""
			},
			wantErr: true,
		},
		{
			give: "nil networkID → error",
			modify: func(w *Wallet) {
				w.NetworkID = id.Nil()
			},
			wantErr: true,
		},
		{
			give: "invalid tier → error",
			modify: func(w *Wallet) {
				w.Tier = "unknown"
			},
			wantErr: true,
		},
		{
			give: "invalid status → error",
			modify: func(w *Wallet) {
				w.Status = "invalid_status"
			},
			wantErr: true,
		},
		{
			give: "pool tier without derivation path → error",
			modify: func(w *Wallet) {
				w.Tier = WalletTierPool
				w.DerivationPath = ""
			},
			wantErr: true,
		},
		{
			give: "hot tier without derivation path → OK",
			modify: func(w *Wallet) {
				w.Tier = WalletTierHot
				w.DerivationPath = ""
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			w := validWallet()
			tt.modify(w)
			err := w.Validate(ctx)

			if tt.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWallet_IsFree(t *testing.T) {
	tests := []struct {
		give     string
		status   WalletStatus
		isActive bool
		want     bool
	}{
		{"free + active → true", WalletStatusFree, true, true},
		{"free + inactive → false", WalletStatusFree, false, false},
		{"leased + active → false", WalletStatusLeased, true, false},
		{"sweep_pending + active → false", WalletStatusSweepPending, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			w := &Wallet{Status: tt.status, IsActive: tt.isActive}
			if got := w.IsFree(); got != tt.want {
				t.Errorf("IsFree() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWallet_IsSystemWallet(t *testing.T) {
	tests := []struct {
		give string
		tier WalletTier
		want bool
	}{
		{"pool → false", WalletTierPool, false},
		{"hot → true", WalletTierHot, true},
		{"warm → true", WalletTierWarm, true},
		{"cold → true", WalletTierCold, true},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			w := &Wallet{Tier: tt.tier}
			if got := w.IsSystemWallet(); got != tt.want {
				t.Errorf("IsSystemWallet() = %v, want %v", got, tt.want)
			}
		})
	}
}
