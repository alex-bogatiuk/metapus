// Package wallet provides the Wallet catalog.
// Wallets are blockchain addresses used for receiving and managing crypto assets.
// Pool wallets are leased to invoices; hot/warm/cold wallets are system-managed.
package wallet

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// WalletStatus defines the availability state of a wallet.
type WalletStatus string

const (
	WalletStatusFree         WalletStatus = "free"          // available for lease
	WalletStatusLeased       WalletStatus = "leased"        // assigned to an invoice
	WalletStatusSweepPending WalletStatus = "sweep_pending" // awaiting sweep to hot wallet
	WalletStatusFrozen       WalletStatus = "frozen"        // manually frozen (compliance)
)

// WalletTier defines the operational tier of a wallet.
type WalletTier string

const (
	WalletTierPool WalletTier = "pool" // client-facing pool (HD-derived)
	WalletTierHot  WalletTier = "hot"  // sweep target, high-frequency operations
	WalletTierWarm WalletTier = "warm" // settlement buffer
	WalletTierCold WalletTier = "cold" // long-term cold storage
)

var _validWalletTiers = map[WalletTier]bool{
	WalletTierPool: true, WalletTierHot: true, WalletTierWarm: true, WalletTierCold: true,
}

var _validWalletStatuses = map[WalletStatus]bool{
	WalletStatusFree: true, WalletStatusLeased: true, WalletStatusSweepPending: true, WalletStatusFrozen: true,
}

// Wallet represents a blockchain address managed by the platform.
type Wallet struct {
	entity.Catalog

	// NetworkID references the blockchain network (FK → cat_blockchain_networks)
	NetworkID id.ID `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`

	// MerchantID is the owning merchant. Nil for system wallets (hot/warm/cold).
	MerchantID *id.ID `db:"merchant_id" json:"merchantId,omitempty" meta:"label:Мерчант,ref:merchant"`

	// Address is the blockchain address string.
	Address string `db:"address" json:"address" meta:"label:Адрес"`

	// DerivationPath is the BIP-44 HD derivation path (e.g., "m/44'/195'/0'/0/42").
	DerivationPath string `db:"derivation_path" json:"derivationPath" meta:"label:Деривация"`

	// Tier defines the wallet's operational tier.
	Tier WalletTier `db:"tier" json:"tier" meta:"label:Уровень"`

	// Status is the current availability state.
	Status WalletStatus `db:"status" json:"status" meta:"label:Статус"`

	// LeasedUntil is the lease expiration time. Nil if not leased.
	LeasedUntil *time.Time `db:"leased_until" json:"leasedUntil,omitempty" meta:"label:Аренда до"`

	// LeasedForID references the invoice this wallet is leased to.
	LeasedForID *id.ID `db:"leased_for_id" json:"leasedForId,omitempty" meta:"label:Арендован для,ref:crypto_invoice"`

	// IsActive enables/disables the wallet.
	IsActive bool `db:"is_active" json:"isActive" meta:"label:Активен"`
}

// NewWallet creates a new pool wallet for a specific network.
func NewWallet(code, name string, networkID id.ID, address, derivationPath string) *Wallet {
	return &Wallet{
		Catalog:        entity.NewCatalog(code, name),
		NetworkID:      networkID,
		Address:        address,
		DerivationPath: derivationPath,
		Tier:           WalletTierPool,
		Status:         WalletStatusFree,
		IsActive:       true,
	}
}

// NewSystemWallet creates a system wallet (hot/warm/cold) without merchant binding.
func NewSystemWallet(code, name string, networkID id.ID, address string, tier WalletTier) *Wallet {
	return &Wallet{
		Catalog:   entity.NewCatalog(code, name),
		NetworkID: networkID,
		Address:   address,
		Tier:      tier,
		Status:    WalletStatusFree,
		IsActive:  true,
	}
}

// Validate implements entity.Validatable.
func (w *Wallet) Validate(ctx context.Context) error {
	if err := w.Catalog.Validate(ctx); err != nil {
		return err
	}

	if id.IsNil(w.NetworkID) {
		return apperror.NewValidation("network is required").
			WithDetail("field", "networkId")
	}

	if w.Address == "" {
		return apperror.NewValidation("address is required").
			WithDetail("field", "address")
	}

	if !_validWalletTiers[w.Tier] {
		return apperror.NewValidation("invalid wallet tier").
			WithDetail("field", "tier")
	}

	if !_validWalletStatuses[w.Status] {
		return apperror.NewValidation("invalid wallet status").
			WithDetail("field", "status")
	}

	// Pool wallets must have a derivation path
	if w.Tier == WalletTierPool && w.DerivationPath == "" {
		return apperror.NewValidation("derivation path is required for pool wallets").
			WithDetail("field", "derivationPath")
	}

	return nil
}

// IsFree returns true if the wallet is available for lease.
func (w *Wallet) IsFree() bool {
	return w.Status == WalletStatusFree && w.IsActive
}

// IsSystemWallet returns true for hot/warm/cold wallets.
func (w *Wallet) IsSystemWallet() bool {
	return w.Tier != WalletTierPool
}

// Lease marks the wallet as leased for a specific invoice until the given time.
func (w *Wallet) Lease(invoiceID id.ID, until time.Time) {
	w.Status = WalletStatusLeased
	w.LeasedForID = &invoiceID
	w.LeasedUntil = &until
}

// Release returns the wallet to the free pool.
func (w *Wallet) Release() {
	w.Status = WalletStatusFree
	w.LeasedForID = nil
	w.LeasedUntil = nil
}

// MarkSweepPending marks the wallet as pending sweep to hot wallet.
func (w *Wallet) MarkSweepPending() {
	w.Status = WalletStatusSweepPending
	w.LeasedForID = nil
	w.LeasedUntil = nil
}
