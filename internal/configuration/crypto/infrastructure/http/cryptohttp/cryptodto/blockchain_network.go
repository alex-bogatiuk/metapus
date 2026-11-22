package cryptodto

import (
	"metapus/internal/configuration/crypto/domain/catalogs/blockchain_network"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/http/dto"
	"metapus/pkg/sdk/types"
)

// --- Request DTOs ---

// CreateBlockchainNetworkRequest is the request body for creating a blockchain network.
type CreateBlockchainNetworkRequest struct {
	RPCConfigSource     string             `json:"rpcConfigSource"`
	Code                string             `json:"code"`
	Name                string             `json:"name" binding:"required"`
	ChainFamily         crypto.ChainFamily `json:"chainFamily" binding:"required"`
	NetworkIdentifier   string             `json:"networkIdentifier" binding:"required"`
	NativeTokenSymbol   string             `json:"nativeTokenSymbol" binding:"required"`
	NativeDecimals      int                `json:"nativeDecimals"`
	ConfirmationsNeeded int                `json:"confirmationsNeeded"`
	BlockTimeSeconds    int                `json:"blockTimeSeconds"`
	ExplorerURL         string             `json:"explorerUrl"`
	Color               types.PaletteColor `json:"color"`
	IsActive            bool               `json:"isActive"`
	ParentID            *string            `json:"parentId"`
	IsFolder            bool               `json:"isFolder"`
	Attributes          entity.Attributes  `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateBlockchainNetworkRequest) ToEntity() *blockchain_network.BlockchainNetwork {
	n := blockchain_network.NewBlockchainNetwork(r.Code, r.Name, r.ChainFamily, r.NetworkIdentifier, r.NativeTokenSymbol, r.NativeDecimals)
	if r.ConfirmationsNeeded > 0 {
		n.ConfirmationsNeeded = r.ConfirmationsNeeded
	}
	if r.BlockTimeSeconds > 0 {
		n.BlockTimeSeconds = r.BlockTimeSeconds
	}
	n.ExplorerURL = r.ExplorerURL
	n.Color = r.Color
	n.RPCConfigSource = r.RPCConfigSource
	if n.RPCConfigSource == "" {
		n.RPCConfigSource = "file"
	}
	n.IsActive = r.IsActive
	n.ParentID = dto.StringPtrToIDPtr(r.ParentID)
	n.IsFolder = r.IsFolder
	n.Attributes = r.Attributes
	return n
}

// UpdateBlockchainNetworkRequest is the request body for updating a blockchain network.
type UpdateBlockchainNetworkRequest struct {
	RPCConfigSource     string             `json:"rpcConfigSource"`
	Code                string             `json:"code"`
	Name                string             `json:"name" binding:"required"`
	ChainFamily         crypto.ChainFamily `json:"chainFamily" binding:"required"`
	NetworkIdentifier   string             `json:"networkIdentifier" binding:"required"`
	NativeTokenSymbol   string             `json:"nativeTokenSymbol" binding:"required"`
	NativeDecimals      int                `json:"nativeDecimals"`
	ConfirmationsNeeded int                `json:"confirmationsNeeded"`
	BlockTimeSeconds    int                `json:"blockTimeSeconds"`
	ExplorerURL         string             `json:"explorerUrl"`
	Color               types.PaletteColor `json:"color"`
	IsActive            bool               `json:"isActive"`
	ParentID            *string            `json:"parentId"`
	IsFolder            bool               `json:"isFolder"`
	Attributes          entity.Attributes  `json:"attributes"`
	Version             int                `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateBlockchainNetworkRequest) ApplyTo(n *blockchain_network.BlockchainNetwork) {
	n.Code = r.Code
	n.Name = r.Name
	n.ChainFamily = r.ChainFamily
	n.NetworkIdentifier = r.NetworkIdentifier
	n.NativeTokenSymbol = r.NativeTokenSymbol
	n.NativeDecimals = r.NativeDecimals
	n.ConfirmationsNeeded = r.ConfirmationsNeeded
	n.BlockTimeSeconds = r.BlockTimeSeconds
	n.ExplorerURL = r.ExplorerURL
	n.Color = r.Color
	if r.RPCConfigSource != "" {
		n.RPCConfigSource = r.RPCConfigSource
	}
	n.IsActive = r.IsActive
	n.ParentID = dto.StringPtrToIDPtr(r.ParentID)
	n.IsFolder = r.IsFolder
	n.Attributes = r.Attributes
	n.Version = r.Version
}

// --- Response DTOs ---

// BlockchainNetworkResponse is the response body for a blockchain network.
type BlockchainNetworkResponse struct {
	RPCConfigSource     string             `json:"rpcConfigSource"`
	ID                  string             `json:"id"`
	Code                string             `json:"code"`
	Name                string             `json:"name"`
	ChainFamily         crypto.ChainFamily `json:"chainFamily"`
	NetworkIdentifier   string             `json:"networkIdentifier"`
	NativeTokenSymbol   string             `json:"nativeTokenSymbol"`
	NativeDecimals      int                `json:"nativeDecimals"`
	ConfirmationsNeeded int                `json:"confirmationsNeeded"`
	BlockTimeSeconds    int                `json:"blockTimeSeconds"`
	ExplorerURL         string             `json:"explorerUrl,omitempty"`
	Color               types.PaletteColor `json:"color"`
	IsActive            bool               `json:"isActive"`
	ParentID            *string            `json:"parentId,omitempty"`
	IsFolder            bool               `json:"isFolder"`
	DeletionMark        bool               `json:"deletionMark"`
	Version             int                `json:"version"`
	Attributes          entity.Attributes  `json:"attributes,omitempty"`
}

// FromBlockchainNetwork creates response DTO from domain entity.
func FromBlockchainNetwork(n *blockchain_network.BlockchainNetwork) *BlockchainNetworkResponse {
	return &BlockchainNetworkResponse{
		RPCConfigSource:     n.RPCConfigSource,
		ID:                  n.ID.String(),
		Code:                n.Code,
		Name:                n.Name,
		ChainFamily:         n.ChainFamily,
		NetworkIdentifier:   n.NetworkIdentifier,
		NativeTokenSymbol:   n.NativeTokenSymbol,
		NativeDecimals:      n.NativeDecimals,
		ConfirmationsNeeded: n.ConfirmationsNeeded,
		BlockTimeSeconds:    n.BlockTimeSeconds,
		ExplorerURL:         n.ExplorerURL,
		Color:               n.Color,
		IsActive:            n.IsActive,
		ParentID:            dto.IDToStringPtr(n.ParentID),
		IsFolder:            n.IsFolder,
		DeletionMark:        n.DeletionMark,
		Version:             n.Version,
		Attributes:          n.Attributes,
	}
}
