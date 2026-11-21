// Package wallet provides the Wallet catalog.
// Wallets are blockchain addresses used for receiving and managing crypto assets.
// Pool wallets are leased to invoices; hot/warm/cold wallets are system-managed.
package wallet

import (
	"context"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// WalletStatus defines the availability state of a wallet.
type WalletStatus string

const (
	WalletStatusFree         WalletStatus = "free"          // available for lease
	WalletStatusLeased       WalletStatus = "leased"        // assigned to an invoice (transient)
	WalletStatusAssigned     WalletStatus = "assigned"      // assigned to a customer (persistent)
	WalletStatusSweepPending WalletStatus = "sweep_pending" // awaiting sweep to hot wallet
	WalletStatusSweeping     WalletStatus = "sweeping"      // claimed by sweep executor
	WalletStatusFrozen       WalletStatus = "frozen"        // manually frozen (compliance)
)

// ChainActivationStatus tracks whether the blockchain account exists on-chain.
type ChainActivationStatus string

const (
	ChainActivationUnknown    ChainActivationStatus = "unknown"
	ChainActivationActivating ChainActivationStatus = "activating"
	ChainActivationActive     ChainActivationStatus = "active"
	ChainActivationHeld       ChainActivationStatus = "held"
	ChainActivationFailed     ChainActivationStatus = "failed"
)

// WalletTier defines the operational tier of a wallet.
type WalletTier string

const (
	WalletTierPool WalletTier = "pool" // client-facing pool (HD-derived)
	WalletTierHot  WalletTier = "hot"  // sweep target, high-frequency operations
	WalletTierWarm WalletTier = "warm" // settlement buffer
	WalletTierCold WalletTier = "cold" // long-term cold storage
)

// WalletRole defines the financial purpose of a managed address independently
// from its custody tier. In particular, a network-fee wallet is never eligible
// for merchant allocation or liquidity routing even though it is a hot wallet.
type WalletRole string

const (
	WalletRoleMerchantPool WalletRole = "merchant_pool"
	WalletRoleLiquidityHot WalletRole = "liquidity_hot"
	WalletRoleNetworkFee   WalletRole = "network_fee"
	WalletRoleTreasury     WalletRole = "treasury"
)

// AllocationMode defines how a pool wallet is assigned.
type AllocationMode string

const (
	// AllocationModeTransient — wallet is leased per invoice, released after payment confirmed.
	AllocationModeTransient AllocationMode = "transient"
	// AllocationModePersistent — wallet is assigned to a customer permanently.
	AllocationModePersistent AllocationMode = "persistent"
)

var _validWalletTiers = map[WalletTier]bool{
	WalletTierPool: true, WalletTierHot: true, WalletTierWarm: true, WalletTierCold: true,
}

var _validWalletRoles = map[WalletRole]bool{
	WalletRoleMerchantPool: true,
	WalletRoleLiquidityHot: true,
	WalletRoleNetworkFee:   true,
	WalletRoleTreasury:     true,
}

var _validWalletStatuses = map[WalletStatus]bool{
	WalletStatusFree: true, WalletStatusLeased: true, WalletStatusAssigned: true,
	WalletStatusSweepPending: true, WalletStatusSweeping: true, WalletStatusFrozen: true,
}

var _validChainActivationStatuses = map[ChainActivationStatus]bool{
	ChainActivationUnknown: true, ChainActivationActivating: true, ChainActivationActive: true,
	ChainActivationHeld: true, ChainActivationFailed: true,
}

var _validAllocationModes = map[AllocationMode]bool{
	AllocationModeTransient: true, AllocationModePersistent: true,
}

// Wallet represents a blockchain address managed by the platform.
type Wallet struct {
	entity.Catalog

	// NetworkID references the blockchain network (FK → cat_blockchain_networks)
	NetworkID id.ID `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`

	// MerchantID is the temporary/persistent customer assignment, separate from custody.
	MerchantID *id.ID `db:"merchant_id" json:"merchantId,omitempty" meta:"label:Мерчант назначения,ref:merchant"`

	CustodyMerchantID *id.ID            `db:"custody_merchant_id" json:"custodyMerchantId,omitempty" meta:"label:Владелец средств,ref:merchant,readOnly:true"`
	RiskDisplay       aml.WalletDisplay `db:"-" json:"-"`

	// Address is the blockchain address string.
	Address string `db:"address" json:"address" meta:"label:Адрес"`

	// AddressKey is the chain-family canonical custody comparison key.
	AddressKey string `db:"address_key" json:"-"`

	// DerivationPath is the BIP-44 HD derivation path (e.g., "m/44'/195'/0'/0/42").
	DerivationPath string `db:"derivation_path" json:"derivationPath" meta:"label:Деривация"`

	// VaultKeyID is the Vault key handle returned by the TRON plugin.
	VaultKeyID string `db:"vault_key_id" json:"vaultKeyId" meta:"label:Vault Key ID,list:false"`

	// Tier defines the wallet's operational tier.
	Tier WalletTier `db:"tier" json:"tier" meta:"label:Уровень"`

	// Role defines whether this address serves merchants, liquidity, fees, or treasury.
	Role WalletRole `db:"wallet_role" json:"walletRole" meta:"label:Роль"`

	// Status is the current availability state.
	Status WalletStatus `db:"status" json:"status" meta:"label:Статус"`

	// LeasedUntil is the lease expiration time. Nil if not leased.
	LeasedUntil *time.Time `db:"leased_until" json:"leasedUntil,omitempty" meta:"label:Аренда до"`

	// LeasedForID references the invoice this wallet is leased to.
	LeasedForID *id.ID `db:"leased_for_id" json:"leasedForId,omitempty" meta:"label:Арендован для,ref:crypto_invoice"`

	// CooldownUntil is the wall-clock time after which a free transient wallet
	// may be leased again. Nil means no reuse quarantine.
	CooldownUntil *time.Time `db:"cooldown_until" json:"cooldownUntil,omitempty" meta:"label:Переиспользование после"`

	// IsActive enables/disables the wallet.
	IsActive bool `db:"is_active" json:"isActive" meta:"label:Активен"`

	// AllocationMode defines whether the wallet is transient (per-invoice) or persistent (per-customer).
	AllocationMode AllocationMode `db:"allocation_mode" json:"allocationMode" meta:"label:Режим аллокации"`

	// CustomerID is the external customer identifier for persistent wallets.
	CustomerID string `db:"customer_id" json:"customerId,omitempty" meta:"label:ID клиента"`

	DepositAssignedAt *time.Time `db:"deposit_assigned_at" json:"depositAssignedAt,omitempty" meta:"label:Адрес выдан,readOnly:true"`
	// Retirement is permanent and independent from the AML assessment. Custody,
	// customer attribution and chain monitoring remain intact.
	DepositRetiredAt *time.Time `db:"deposit_retired_at" json:"depositRetiredAt,omitempty" meta:"label:Адрес выведен из обращения,readOnly:true"`

	// LastSweptAt is when the wallet was last swept. Used for max-age sweep evaluation.
	LastSweptAt *time.Time `db:"last_swept_at" json:"lastSweptAt,omitempty" meta:"label:Последний свип"`

	// ChainActivationStatus tracks whether the address/account is usable on-chain.
	ChainActivationStatus ChainActivationStatus `db:"chain_activation_status" json:"chainActivationStatus" meta:"label:Активация в сети"`

	// AllocationHeld and AllocationHoldReasons are computed projections from
	// independent reg_crypto_wallet_allocation_holds records.
	AllocationHeld        bool     `db:"-" json:"allocationHeld" meta:"label:Аллокация заблокирована"`
	AllocationHoldReasons []string `db:"-" json:"allocationHoldReasons,omitempty" meta:"label:Причины блокировки аллокации,list:false"`
}

// NewWallet creates a new pool wallet for a specific network.
func NewWallet(code, name string, networkID id.ID, address, derivationPath, vaultKeyID string) *Wallet {
	return &Wallet{
		Catalog:               entity.NewCatalog(code, name),
		NetworkID:             networkID,
		Address:               address,
		DerivationPath:        derivationPath,
		VaultKeyID:            vaultKeyID,
		Tier:                  WalletTierPool,
		Role:                  WalletRoleMerchantPool,
		Status:                WalletStatusFree,
		AllocationMode:        AllocationModeTransient,
		IsActive:              true,
		ChainActivationStatus: ChainActivationUnknown,
	}
}

// NewSystemWallet creates a system wallet (hot/warm/cold) without merchant binding.
func NewSystemWallet(code, name string, networkID id.ID, address, vaultKeyID string, tier WalletTier) *Wallet {
	role := WalletRoleTreasury
	if tier == WalletTierHot {
		role = WalletRoleLiquidityHot
	}
	return &Wallet{
		Catalog:               entity.NewCatalog(code, name),
		NetworkID:             networkID,
		Address:               address,
		VaultKeyID:            vaultKeyID,
		Tier:                  tier,
		Role:                  role,
		Status:                WalletStatusFree,
		AllocationMode:        AllocationModeTransient,
		IsActive:              true,
		ChainActivationStatus: ChainActivationActive,
	}
}

// Validate implements entity.Validatable.
func (w *Wallet) Validate(ctx context.Context) error {
	normalized := *w
	normalized.Normalize()
	w = &normalized
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

	if w.VaultKeyID == "" {
		return apperror.NewValidation("vault key id is required").
			WithDetail("field", "vaultKeyId")
	}

	if !_validWalletTiers[w.Tier] {
		return apperror.NewValidation("invalid wallet tier").
			WithDetail("field", "tier")
	}

	if !_validWalletRoles[w.Role] {
		return apperror.NewValidation("invalid wallet role").
			WithDetail("field", "walletRole")
	}
	if w.Role == WalletRoleMerchantPool && w.Tier != WalletTierPool {
		return apperror.NewValidation("merchant pool wallet must use pool tier").
			WithDetail("field", "walletRole")
	}
	if (w.Role == WalletRoleLiquidityHot || w.Role == WalletRoleNetworkFee) && w.Tier != WalletTierHot {
		return apperror.NewValidation("liquidity and fee wallets must use hot tier").
			WithDetail("field", "walletRole")
	}
	if w.Role == WalletRoleTreasury && w.Tier == WalletTierPool {
		return apperror.NewValidation("treasury wallet cannot use pool tier").
			WithDetail("field", "walletRole")
	}

	if !_validWalletStatuses[w.Status] {
		return apperror.NewValidation("invalid wallet status").
			WithDetail("field", "status")
	}

	if !_validChainActivationStatuses[w.ChainActivationStatus] {
		return apperror.NewValidation("invalid chain activation status").
			WithDetail("field", "chainActivationStatus")
	}

	if !_validAllocationModes[w.AllocationMode] {
		return apperror.NewValidation("invalid allocation mode").
			WithDetail("field", "allocationMode")
	}

	// Pool wallets must have a derivation path
	if w.Tier == WalletTierPool && w.DerivationPath == "" {
		return apperror.NewValidation("derivation path is required for pool wallets").
			WithDetail("field", "derivationPath")
	}

	// Persistent wallets must have a customer ID
	if w.AllocationMode == AllocationModePersistent && w.CustomerID == "" {
		return apperror.NewValidation("customer id is required for persistent wallets").
			WithDetail("field", "customerId")
	}

	return nil
}

// IsFree returns true if the wallet is available for lease.
func (w *Wallet) IsFree() bool {
	if w.Status != WalletStatusFree || !w.IsActive || w.AllocationHeld || w.DepositRetiredAt != nil {
		return false
	}
	return w.Tier != WalletTierPool || w.ChainActivationStatus == ChainActivationActive
}

// IsSystemWallet returns true for hot/warm/cold wallets.
func (w *Wallet) IsSystemWallet() bool {
	return w.Tier != WalletTierPool
}

// IsTransient returns true if this wallet uses per-invoice allocation.
func (w *Wallet) IsTransient() bool {
	return w.AllocationMode == AllocationModeTransient
}

// IsPersistent returns true if this wallet is permanently assigned to a customer.
func (w *Wallet) IsPersistent() bool {
	return w.AllocationMode == AllocationModePersistent
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

// Normalize fills defaults before validation and persistence.
func (w *Wallet) Normalize() {
	if w.Role == "" {
		switch w.Tier {
		case WalletTierPool:
			w.Role = WalletRoleMerchantPool
		case WalletTierHot:
			w.Role = WalletRoleLiquidityHot
		default:
			w.Role = WalletRoleTreasury
		}
	}
	if w.ChainActivationStatus == "" {
		w.ChainActivationStatus = ChainActivationUnknown
	}
}
