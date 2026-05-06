package dto

import (
	"time"

	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/infrastructure/storage/postgres"
)

// CryptoSweep is system-only — no Create/Update DTOs needed.

// CryptoSweepResponse is the response body for a sweep.
type CryptoSweepResponse struct {
	ID          string               `json:"id"`
	Number      string               `json:"number"`
	Date        string               `json:"date"`
	TokenID     string               `json:"tokenId"`
	HotWalletID string               `json:"hotWalletId"`
	TotalAmount string               `json:"totalAmount"`
	TotalFee    string               `json:"totalFee"`
	Status      string               `json:"status"`
	StatusName  string               `json:"statusName"`
	LineCount   int                  `json:"lineCount"`
	Posted      bool                 `json:"posted"`
	DeletionMark bool               `json:"deletionMark"`
	Version     int                  `json:"version"`

	// Resolved references
	Token *postgres.RefDisplay `json:"token,omitempty"`
}

// FromCryptoSweep creates response DTO from domain entity.
func FromCryptoSweep(s *crypto_sweep.CryptoSweep, refs ...postgres.ResolvedRefs) *CryptoSweepResponse {
	resp := &CryptoSweepResponse{
		ID:          s.ID.String(),
		Number:      s.Number,
		Date:        s.Date.Format(time.RFC3339),
		TokenID:     s.TokenID.String(),
		HotWalletID: s.HotWalletID.String(),
		TotalAmount: s.TotalAmount.String(),
		TotalFee:    s.TotalFee.String(),
		Status:      string(s.Status),
		StatusName:  string(s.Status),
		LineCount:   len(s.Lines),
		Posted:      s.Posted,
		DeletionMark: s.DeletionMark,
		Version:     s.Version,
	}

	if len(refs) > 0 {
		tok := refs[0].Get(TableTokens, s.TokenID)
		resp.Token = &tok
	}

	return resp
}

// CollectCryptoSweepRefs collects FK references for batch resolution.
func CollectCryptoSweepRefs(resolver *postgres.ReferenceResolver, s *crypto_sweep.CryptoSweep) {
	resolver.Add(TableTokens, s.TokenID)
}

// Stub DTOs for BaseDocumentHandler type compliance.

type CreateCryptoSweepRequest struct{}

func (r *CreateCryptoSweepRequest) ToEntity() *crypto_sweep.CryptoSweep {
	return &crypto_sweep.CryptoSweep{}
}

type UpdateCryptoSweepRequest struct {
	Version int `json:"version" binding:"required"`
}

func (r *UpdateCryptoSweepRequest) ApplyTo(s *crypto_sweep.CryptoSweep) {
	s.Version = r.Version
}
