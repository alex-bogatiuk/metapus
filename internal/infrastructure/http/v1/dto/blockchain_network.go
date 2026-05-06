package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/blockchain_network"
)

// --- Request DTOs ---

// CreateBlockchainNetworkRequest is the request body for creating a blockchain network.
type CreateBlockchainNetworkRequest struct {
	Code                string            `json:"code"`
	Name                string            `json:"name" binding:"required"`
	ChainID             string            `json:"chainId" binding:"required"`
	NativeTokenSymbol   string            `json:"nativeTokenSymbol" binding:"required"`
	NativeDecimals      int               `json:"nativeDecimals"`
	ConfirmationsNeeded int               `json:"confirmationsNeeded"`
	BlockTimeSeconds    int               `json:"blockTimeSeconds"`
	ExplorerURL         string            `json:"explorerUrl"`
	IsActive            bool              `json:"isActive"`
	ParentID            *string           `json:"parentId"`
	IsFolder            bool              `json:"isFolder"`
	Attributes          entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateBlockchainNetworkRequest) ToEntity() *blockchain_network.BlockchainNetwork {
	n := blockchain_network.NewBlockchainNetwork(r.Code, r.Name, r.ChainID, r.NativeTokenSymbol, r.NativeDecimals)
	if r.ConfirmationsNeeded > 0 {
		n.ConfirmationsNeeded = r.ConfirmationsNeeded
	}
	if r.BlockTimeSeconds > 0 {
		n.BlockTimeSeconds = r.BlockTimeSeconds
	}
	n.ExplorerURL = r.ExplorerURL
	n.IsActive = r.IsActive
	n.ParentID = stringPtrToIDPtr(r.ParentID)
	n.IsFolder = r.IsFolder
	n.Attributes = r.Attributes
	return n
}

// UpdateBlockchainNetworkRequest is the request body for updating a blockchain network.
type UpdateBlockchainNetworkRequest struct {
	Code                string            `json:"code"`
	Name                string            `json:"name" binding:"required"`
	ChainID             string            `json:"chainId" binding:"required"`
	NativeTokenSymbol   string            `json:"nativeTokenSymbol" binding:"required"`
	NativeDecimals      int               `json:"nativeDecimals"`
	ConfirmationsNeeded int               `json:"confirmationsNeeded"`
	BlockTimeSeconds    int               `json:"blockTimeSeconds"`
	ExplorerURL         string            `json:"explorerUrl"`
	IsActive            bool              `json:"isActive"`
	ParentID            *string           `json:"parentId"`
	IsFolder            bool              `json:"isFolder"`
	Attributes          entity.Attributes `json:"attributes"`
	Version             int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateBlockchainNetworkRequest) ApplyTo(n *blockchain_network.BlockchainNetwork) {
	n.Code = r.Code
	n.Name = r.Name
	n.ChainID = r.ChainID
	n.NativeTokenSymbol = r.NativeTokenSymbol
	n.NativeDecimals = r.NativeDecimals
	n.ConfirmationsNeeded = r.ConfirmationsNeeded
	n.BlockTimeSeconds = r.BlockTimeSeconds
	n.ExplorerURL = r.ExplorerURL
	n.IsActive = r.IsActive
	n.ParentID = stringPtrToIDPtr(r.ParentID)
	n.IsFolder = r.IsFolder
	n.Attributes = r.Attributes
	n.Version = r.Version
}

// --- Response DTOs ---

// BlockchainNetworkResponse is the response body for a blockchain network.
type BlockchainNetworkResponse struct {
	ID                  string            `json:"id"`
	Code                string            `json:"code"`
	Name                string            `json:"name"`
	ChainID             string            `json:"chainId"`
	NativeTokenSymbol   string            `json:"nativeTokenSymbol"`
	NativeDecimals      int               `json:"nativeDecimals"`
	ConfirmationsNeeded int               `json:"confirmationsNeeded"`
	BlockTimeSeconds    int               `json:"blockTimeSeconds"`
	ExplorerURL         string            `json:"explorerUrl,omitempty"`
	IsActive            bool              `json:"isActive"`
	ParentID            *string           `json:"parentId,omitempty"`
	IsFolder            bool              `json:"isFolder"`
	DeletionMark        bool              `json:"deletionMark"`
	Version             int               `json:"version"`
	Attributes          entity.Attributes `json:"attributes,omitempty"`
}

// FromBlockchainNetwork creates response DTO from domain entity.
func FromBlockchainNetwork(n *blockchain_network.BlockchainNetwork) *BlockchainNetworkResponse {
	return &BlockchainNetworkResponse{
		ID:                  n.ID.String(),
		Code:                n.Code,
		Name:                n.Name,
		ChainID:             n.ChainID,
		NativeTokenSymbol:   n.NativeTokenSymbol,
		NativeDecimals:      n.NativeDecimals,
		ConfirmationsNeeded: n.ConfirmationsNeeded,
		BlockTimeSeconds:    n.BlockTimeSeconds,
		ExplorerURL:         n.ExplorerURL,
		IsActive:            n.IsActive,
		ParentID:            idToStringPtr(n.ParentID),
		IsFolder:            n.IsFolder,
		DeletionMark:        n.DeletionMark,
		Version:             n.Version,
		Attributes:          n.Attributes,
	}
}
