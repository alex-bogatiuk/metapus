package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/infrastructure/storage/postgres"
)

// --- Request DTOs ---

// CreateTokenRequest is the request body for creating a token.
type CreateTokenRequest struct {
	Code             string            `json:"code"`
	Name             string            `json:"name" binding:"required"`
	NetworkID        string            `json:"networkId" binding:"required"`
	ContractAddress  string            `json:"contractAddress"`
	Symbol           string            `json:"symbol" binding:"required"`
	DecimalPlaces    int               `json:"decimalPlaces"`
	Standard         string            `json:"tokenStandard" binding:"required"`
	IsActive         bool              `json:"isActive"`
	SweepThreshold   string            `json:"sweepThreshold"`
	SweepMaxAgeHours int               `json:"sweepMaxAgeHours"`
	ParentID         *string           `json:"parentId"`
	IsFolder         bool              `json:"isFolder"`
	Attributes       entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateTokenRequest) ToEntity() *token.Token {
	networkID, _ := id.Parse(r.NetworkID)
	t := token.NewToken(r.Code, r.Name, networkID, r.Symbol, r.DecimalPlaces, token.TokenStandard(r.Standard))
	t.ContractAddress = r.ContractAddress
	t.IsActive = r.IsActive
	t.SweepThreshold, _ = types.NewCryptoAmountFromString(r.SweepThreshold)
	t.SweepMaxAgeHours = r.SweepMaxAgeHours
	t.ParentID = stringPtrToIDPtr(r.ParentID)
	t.IsFolder = r.IsFolder
	t.Attributes = r.Attributes
	return t
}

// UpdateTokenRequest is the request body for updating a token.
type UpdateTokenRequest struct {
	Code             string            `json:"code"`
	Name             string            `json:"name" binding:"required"`
	NetworkID        string            `json:"networkId" binding:"required"`
	ContractAddress  string            `json:"contractAddress"`
	Symbol           string            `json:"symbol" binding:"required"`
	DecimalPlaces    int               `json:"decimalPlaces"`
	Standard         string            `json:"tokenStandard" binding:"required"`
	IsActive         bool              `json:"isActive"`
	SweepThreshold   string            `json:"sweepThreshold"`
	SweepMaxAgeHours int               `json:"sweepMaxAgeHours"`
	ParentID         *string           `json:"parentId"`
	IsFolder         bool              `json:"isFolder"`
	Attributes       entity.Attributes `json:"attributes"`
	Version          int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateTokenRequest) ApplyTo(t *token.Token) {
	t.Code = r.Code
	t.Name = r.Name
	networkID, _ := id.Parse(r.NetworkID)
	t.NetworkID = networkID
	t.ContractAddress = r.ContractAddress
	t.Symbol = r.Symbol
	t.DecimalPlaces = r.DecimalPlaces
	t.Standard = token.TokenStandard(r.Standard)
	t.IsActive = r.IsActive
	t.SweepThreshold, _ = types.NewCryptoAmountFromString(r.SweepThreshold)
	t.SweepMaxAgeHours = r.SweepMaxAgeHours
	t.ParentID = stringPtrToIDPtr(r.ParentID)
	t.IsFolder = r.IsFolder
	t.Attributes = r.Attributes
	t.Version = r.Version
}

// --- Response DTOs ---

// CryptoTokenResponse is the response body for a token.
type CryptoTokenResponse struct {
	ID               string            `json:"id"`
	Code             string            `json:"code"`
	Name             string            `json:"name"`
	NetworkID        string            `json:"networkId"`
	ContractAddress  string            `json:"contractAddress"`
	Symbol           string            `json:"symbol"`
	DecimalPlaces    int               `json:"decimalPlaces"`
	Standard         string            `json:"tokenStandard"`
	IsActive         bool              `json:"isActive"`
	SweepThreshold   string            `json:"sweepThreshold"`
	SweepMaxAgeHours int               `json:"sweepMaxAgeHours"`
	ParentID         *string           `json:"parentId,omitempty"`
	IsFolder         bool              `json:"isFolder"`
	DeletionMark     bool              `json:"deletionMark"`
	Version          int               `json:"version"`
	Attributes       entity.Attributes `json:"attributes,omitempty"`

	// Resolved references
	Network *postgres.RefDisplay `json:"network,omitempty"`
}

// FromToken creates response DTO from domain entity.
// Accepts optional resolved refs for network name.
func FromToken(t *token.Token, refs ...postgres.ResolvedRefs) *CryptoTokenResponse {
	resp := &CryptoTokenResponse{
		ID:               t.ID.String(),
		Code:             t.Code,
		Name:             t.Name,
		NetworkID:        t.NetworkID.String(),
		ContractAddress:  t.ContractAddress,
		Symbol:           t.Symbol,
		DecimalPlaces:    t.DecimalPlaces,
		Standard:         string(t.Standard),
		IsActive:         t.IsActive,
		SweepThreshold:   t.SweepThreshold.String(),
		SweepMaxAgeHours: t.SweepMaxAgeHours,
		ParentID:         idToStringPtr(t.ParentID),
		IsFolder:         t.IsFolder,
		DeletionMark:     t.DeletionMark,
		Version:          t.Version,
		Attributes:       t.Attributes,
	}

	if len(refs) > 0 {
		net := refs[0].Get(TableBlockchainNetworks, t.NetworkID)
		resp.Network = &net
	}

	return resp
}

// CollectTokenRefs collects FK references for batch resolution.
func CollectTokenRefs(resolver *postgres.ReferenceResolver, t *token.Token) {
	resolver.Add(TableBlockchainNetworks, t.NetworkID)
}
