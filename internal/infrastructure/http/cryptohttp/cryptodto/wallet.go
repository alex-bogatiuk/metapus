package cryptodto

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// --- Request DTOs ---

// CreateWalletRequest is the request body for creating a wallet.
type CreateWalletRequest struct {
	Code           string            `json:"code"`
	Name           string            `json:"name" binding:"required"`
	NetworkID      string            `json:"networkId" binding:"required"`
	MerchantID     *string           `json:"merchantId"`
	Address        string            `json:"address" binding:"required"`
	DerivationPath string            `json:"derivationPath"`
	VaultKeyID     string            `json:"vaultKeyId" binding:"required"`
	Tier           string            `json:"tier" binding:"required"`
	IsActive       bool              `json:"isActive"`
	ParentID       *string           `json:"parentId"`
	IsFolder       bool              `json:"isFolder"`
	Attributes     entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateWalletRequest) ToEntity() *wallet.Wallet {
	networkID, _ := id.Parse(r.NetworkID)
	w := wallet.NewWallet(r.Code, r.Name, networkID, r.Address, r.DerivationPath, r.VaultKeyID)
	w.Tier = wallet.WalletTier(r.Tier)
	if w.Tier != wallet.WalletTierPool {
		w.ChainActivationStatus = wallet.ChainActivationActive
	}
	w.IsActive = r.IsActive
	w.MerchantID = stringPtrToID(r.MerchantID)
	w.ParentID = dto.StringPtrToIDPtr(r.ParentID)
	w.IsFolder = r.IsFolder
	w.Attributes = r.Attributes
	return w
}

// UpdateWalletRequest is the request body for updating a wallet.
type UpdateWalletRequest struct {
	Code                  string            `json:"code"`
	Name                  string            `json:"name" binding:"required"`
	NetworkID             string            `json:"networkId" binding:"required"`
	MerchantID            *string           `json:"merchantId"`
	Address               string            `json:"address" binding:"required"`
	DerivationPath        string            `json:"derivationPath"`
	VaultKeyID            string            `json:"vaultKeyId" binding:"required"`
	Tier                  string            `json:"tier" binding:"required"`
	Status                string            `json:"status"`
	AllocationMode        string            `json:"allocationMode"`
	CustomerID            string            `json:"customerId"`
	IsActive              bool              `json:"isActive"`
	LastSweptAt           *time.Time        `json:"lastSweptAt,omitempty"`
	ChainActivationStatus string            `json:"chainActivationStatus"`
	ParentID              *string           `json:"parentId"`
	IsFolder              bool              `json:"isFolder"`
	Attributes            entity.Attributes `json:"attributes"`
	Version               int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateWalletRequest) ApplyTo(w *wallet.Wallet) {
	w.Code = r.Code
	w.Name = r.Name
	networkID, _ := id.Parse(r.NetworkID)
	w.NetworkID = networkID
	w.MerchantID = stringPtrToID(r.MerchantID)
	w.Address = r.Address
	w.DerivationPath = r.DerivationPath
	w.VaultKeyID = r.VaultKeyID
	w.Tier = wallet.WalletTier(r.Tier)
	if r.Status != "" {
		w.Status = wallet.WalletStatus(r.Status)
	}
	if r.AllocationMode != "" {
		w.AllocationMode = wallet.AllocationMode(r.AllocationMode)
	}
	w.CustomerID = r.CustomerID
	w.IsActive = r.IsActive
	w.LastSweptAt = r.LastSweptAt // nil clears the field
	if r.ChainActivationStatus != "" {
		w.ChainActivationStatus = wallet.ChainActivationStatus(r.ChainActivationStatus)
	}
	w.ParentID = dto.StringPtrToIDPtr(r.ParentID)
	w.IsFolder = r.IsFolder
	if r.Attributes != nil {
		w.Attributes = r.Attributes
	} else if w.Attributes == nil {
		w.Attributes = make(entity.Attributes)
	}
	w.Version = r.Version
}

// --- Response DTOs ---

// WalletResponse is the response body for a wallet.
type WalletResponse struct {
	CustodyMerchantID *string                  `json:"custodyMerchantId,omitempty"`
	CustodyMerchant   *postgres.RefDisplay     `json:"custodyMerchant,omitempty"`
	RiskClass         string                   `json:"riskClass"`
	RiskEnabled       bool                     `json:"riskEnabled"`
	RiskHasFunds      bool                     `json:"riskHasFunds"`
	RiskExpiresAt     *time.Time               `json:"riskExpiresAt,omitempty"`
	RiskAssets        []aml.WalletAssetDisplay `json:"riskAssets"`
	DepositAssignedAt *time.Time               `json:"depositAssignedAt,omitempty"`
	DepositRetiredAt  *time.Time               `json:"depositRetiredAt,omitempty"`

	ID                    string            `json:"id"`
	Code                  string            `json:"code"`
	Name                  string            `json:"name"`
	NetworkID             string            `json:"networkId"`
	MerchantID            *string           `json:"merchantId,omitempty"`
	Address               string            `json:"address"`
	DerivationPath        string            `json:"derivationPath,omitempty"`
	VaultKeyID            string            `json:"vaultKeyId"`
	Tier                  string            `json:"tier"`
	TierName              string            `json:"tierName"`
	WalletRole            string            `json:"walletRole"`
	Status                string            `json:"status"`
	StatusName            string            `json:"statusName"`
	AllocationMode        string            `json:"allocationMode"`
	CustomerID            string            `json:"customerId,omitempty"`
	LeasedUntil           *string           `json:"leasedUntil,omitempty"`
	LeasedForID           *string           `json:"leasedForId,omitempty"`
	CooldownUntil         *string           `json:"cooldownUntil,omitempty"`
	LastSweptAt           *string           `json:"lastSweptAt,omitempty"`
	ChainActivationStatus string            `json:"chainActivationStatus"`
	AllocationHeld        bool              `json:"allocationHeld"`
	AllocationHoldReasons []string          `json:"allocationHoldReasons,omitempty"`
	IsActive              bool              `json:"isActive"`
	ParentID              *string           `json:"parentId,omitempty"`
	IsFolder              bool              `json:"isFolder"`
	DeletionMark          bool              `json:"deletionMark"`
	Version               int               `json:"version"`
	Attributes            entity.Attributes `json:"attributes,omitempty"`

	// Resolved references
	Network  *postgres.RefDisplay `json:"network,omitempty"`
	Merchant *postgres.RefDisplay `json:"merchant,omitempty"`
}

// FromWallet creates response DTO from domain entity.
func FromWallet(w *wallet.Wallet, refs ...postgres.ResolvedRefs) *WalletResponse {
	resp := &WalletResponse{
		CustodyMerchantID: dto.IDToStringPtr(w.CustodyMerchantID),
		RiskClass:         string(w.RiskDisplay.Class), RiskEnabled: w.RiskDisplay.Enabled, RiskHasFunds: w.RiskDisplay.HasFunds, RiskExpiresAt: w.RiskDisplay.ExpiresAt, RiskAssets: w.RiskDisplay.Assets,
		DepositAssignedAt: w.DepositAssignedAt, DepositRetiredAt: w.DepositRetiredAt,
		ID:                    w.ID.String(),
		Code:                  w.Code,
		Name:                  w.Name,
		NetworkID:             w.NetworkID.String(),
		MerchantID:            dto.IDToStringPtr(w.MerchantID),
		Address:               w.Address,
		DerivationPath:        w.DerivationPath,
		VaultKeyID:            w.VaultKeyID,
		Tier:                  string(w.Tier),
		TierName:              string(w.Tier),
		WalletRole:            string(w.Role),
		Status:                string(w.Status),
		StatusName:            string(w.Status),
		AllocationMode:        string(w.AllocationMode),
		CustomerID:            w.CustomerID,
		ChainActivationStatus: string(w.ChainActivationStatus),
		AllocationHeld:        w.AllocationHeld,
		AllocationHoldReasons: w.AllocationHoldReasons,
		IsActive:              w.IsActive,
		ParentID:              dto.IDToStringPtr(w.ParentID),
		IsFolder:              w.IsFolder,
		DeletionMark:          w.DeletionMark,
		Version:               w.Version,
		Attributes:            w.Attributes,
	}

	if w.LeasedUntil != nil {
		s := w.LeasedUntil.Format("2006-01-02T15:04:05Z")
		resp.LeasedUntil = &s
	}
	resp.LeasedForID = dto.IDToStringPtr(w.LeasedForID)
	if w.CooldownUntil != nil {
		s := w.CooldownUntil.Format(time.RFC3339)
		resp.CooldownUntil = &s
	}
	if w.LastSweptAt != nil {
		s := w.LastSweptAt.Format(time.RFC3339)
		resp.LastSweptAt = &s
	}
	if len(refs) > 0 {
		net := refs[0].Get(TableBlockchainNetworks, w.NetworkID)
		resp.Network = &net
		if w.CustodyMerchantID != nil {
			owner := refs[0].Get(TableMerchants, *w.CustodyMerchantID)
			resp.CustodyMerchant = &owner
		}
		if w.MerchantID != nil {
			merch := refs[0].Get(TableMerchants, *w.MerchantID)
			resp.Merchant = &merch
		}
	}

	if resp.RiskAssets == nil {
		resp.RiskAssets = []aml.WalletAssetDisplay{}
	}
	if resp.RiskClass == "" {
		resp.RiskClass = "unknown"
	}
	return resp
}

// CollectWalletRefs collects FK references for batch resolution.
func CollectWalletRefs(resolver *postgres.ReferenceResolver, w *wallet.Wallet) {
	if w.CustodyMerchantID != nil {
		resolver.Add(TableMerchants, *w.CustodyMerchantID)
	}
	resolver.Add(TableBlockchainNetworks, w.NetworkID)
	if w.MerchantID != nil {
		resolver.Add(TableMerchants, *w.MerchantID)
	}
}

// stringPtrToID converts a string pointer to an ID pointer.
func stringPtrToID(s *string) *id.ID {
	if s == nil {
		return nil
	}
	parsed, err := id.Parse(*s)
	if err != nil {
		return nil
	}
	return &parsed
}
