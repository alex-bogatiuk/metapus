package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/wallet"
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
	Tier           string            `json:"tier" binding:"required"`
	IsActive       bool              `json:"isActive"`
	ParentID       *string           `json:"parentId"`
	IsFolder       bool              `json:"isFolder"`
	Attributes     entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateWalletRequest) ToEntity() *wallet.Wallet {
	networkID, _ := id.Parse(r.NetworkID)
	w := wallet.NewWallet(r.Code, r.Name, networkID, r.Address, r.DerivationPath)
	w.Tier = wallet.WalletTier(r.Tier)
	w.IsActive = r.IsActive
	w.MerchantID = stringPtrToID(r.MerchantID)
	w.ParentID = stringPtrToIDPtr(r.ParentID)
	w.IsFolder = r.IsFolder
	w.Attributes = r.Attributes
	return w
}

// UpdateWalletRequest is the request body for updating a wallet.
type UpdateWalletRequest struct {
	Code           string            `json:"code"`
	Name           string            `json:"name" binding:"required"`
	NetworkID      string            `json:"networkId" binding:"required"`
	MerchantID     *string           `json:"merchantId"`
	Address        string            `json:"address" binding:"required"`
	DerivationPath string            `json:"derivationPath"`
	Tier           string            `json:"tier" binding:"required"`
	Status         string            `json:"status"`
	AllocationMode string            `json:"allocationMode"`
	CustomerRef    string            `json:"customerRef"`
	IsActive       bool              `json:"isActive"`
	ParentID       *string           `json:"parentId"`
	IsFolder       bool              `json:"isFolder"`
	Attributes     entity.Attributes `json:"attributes"`
	Version        int               `json:"version" binding:"required"`
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
	w.Tier = wallet.WalletTier(r.Tier)
	if r.Status != "" {
		w.Status = wallet.WalletStatus(r.Status)
	}
	if r.AllocationMode != "" {
		w.AllocationMode = wallet.AllocationMode(r.AllocationMode)
	}
	w.CustomerRef = r.CustomerRef
	w.IsActive = r.IsActive
	w.ParentID = stringPtrToIDPtr(r.ParentID)
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
	ID             string            `json:"id"`
	Code           string            `json:"code"`
	Name           string            `json:"name"`
	NetworkID      string            `json:"networkId"`
	MerchantID     *string           `json:"merchantId,omitempty"`
	Address        string            `json:"address"`
	DerivationPath string            `json:"derivationPath,omitempty"`
	Tier           string            `json:"tier"`
	TierName       string            `json:"tierName"`
	Status         string            `json:"status"`
	StatusName     string            `json:"statusName"`
	AllocationMode string            `json:"allocationMode"`
	CustomerRef    string            `json:"customerRef,omitempty"`
	LeasedUntil    *string           `json:"leasedUntil,omitempty"`
	LeasedForID    *string           `json:"leasedForId,omitempty"`
	IsActive       bool              `json:"isActive"`
	ParentID       *string           `json:"parentId,omitempty"`
	IsFolder       bool              `json:"isFolder"`
	DeletionMark   bool              `json:"deletionMark"`
	Version        int               `json:"version"`
	Attributes     entity.Attributes `json:"attributes,omitempty"`

	// Resolved references
	Network  *postgres.RefDisplay `json:"network,omitempty"`
	Merchant *postgres.RefDisplay `json:"merchant,omitempty"`
}



// FromWallet creates response DTO from domain entity.
func FromWallet(w *wallet.Wallet, refs ...postgres.ResolvedRefs) *WalletResponse {
	resp := &WalletResponse{
		ID:             w.ID.String(),
		Code:           w.Code,
		Name:           w.Name,
		NetworkID:      w.NetworkID.String(),
		MerchantID:     idToStringPtr(w.MerchantID),
		Address:        w.Address,
		DerivationPath: w.DerivationPath,
		Tier:           string(w.Tier),
		TierName:       string(w.Tier),
		Status:         string(w.Status),
		StatusName:     string(w.Status),
		AllocationMode: string(w.AllocationMode),
		CustomerRef:    w.CustomerRef,
		IsActive:       w.IsActive,
		ParentID:       idToStringPtr(w.ParentID),
		IsFolder:       w.IsFolder,
		DeletionMark:   w.DeletionMark,
		Version:        w.Version,
		Attributes:     w.Attributes,
	}

	if w.LeasedUntil != nil {
		s := w.LeasedUntil.Format("2006-01-02T15:04:05Z")
		resp.LeasedUntil = &s
	}
	resp.LeasedForID = idToStringPtr(w.LeasedForID)

	if len(refs) > 0 {
		net := refs[0].Get(TableBlockchainNetworks, w.NetworkID)
		resp.Network = &net
		if w.MerchantID != nil {
			merch := refs[0].Get(TableMerchants, *w.MerchantID)
			resp.Merchant = &merch
		}
	}

	return resp
}

// CollectWalletRefs collects FK references for batch resolution.
func CollectWalletRefs(resolver *postgres.ReferenceResolver, w *wallet.Wallet) {
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
