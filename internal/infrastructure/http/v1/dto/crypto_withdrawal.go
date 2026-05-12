package dto

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/infrastructure/storage/postgres"
)

// --- Request DTOs ---

// CreateCryptoWithdrawalRequest is the request body for creating a withdrawal.
type CreateCryptoWithdrawalRequest struct {
	Date           time.Time `json:"date" binding:"required"`
	MerchantID     string    `json:"merchantId" binding:"required"`
	TokenID        string    `json:"tokenId" binding:"required"`
	SourceWalletID string    `json:"sourceWalletId" binding:"required"`
	DestAddress    string    `json:"destAddress" binding:"required"`
	Amount         string    `json:"amount" binding:"required"`
	Description    string    `json:"description,omitempty"`
}

// ToEntity converts request to domain entity.
func (r *CreateCryptoWithdrawalRequest) ToEntity() *crypto_withdrawal.CryptoWithdrawal {
	merchantID, _ := id.Parse(r.MerchantID)
	tokenID, _ := id.Parse(r.TokenID)
	sourceWalletID, _ := id.Parse(r.SourceWalletID)
	amount, _ := types.NewCryptoAmountFromString(r.Amount)

	doc := crypto_withdrawal.NewCryptoWithdrawal(merchantID, tokenID, sourceWalletID, r.DestAddress, amount)
	doc.Date = r.Date
	doc.Description = r.Description
	return doc
}

// UpdateCryptoWithdrawalRequest is the request body for updating a withdrawal.
type UpdateCryptoWithdrawalRequest struct {
	DestAddress string `json:"destAddress,omitempty"`
	Amount      string `json:"amount,omitempty"`
	Description string `json:"description,omitempty"`
	Version     int    `json:"version" binding:"required"`
}

// ApplyTo applies updates to an existing entity.
func (r *UpdateCryptoWithdrawalRequest) ApplyTo(w *crypto_withdrawal.CryptoWithdrawal) {
	if r.DestAddress != "" {
		w.DestAddress = r.DestAddress
	}
	if r.Amount != "" {
		if amt, err := types.NewCryptoAmountFromString(r.Amount); err == nil {
			w.Amount = amt
		}
	}
	if r.Description != "" {
		w.Description = r.Description
	}
	w.Version = r.Version
}

// --- Response DTO ---

// CryptoWithdrawalResponse is the response body for a withdrawal.
type CryptoWithdrawalResponse struct {
	ID             string  `json:"id"`
	Number         string  `json:"number"`
	Date           string  `json:"date"`
	MerchantID     string  `json:"merchantId"`
	TokenID        string  `json:"tokenId"`
	SourceWalletID string  `json:"sourceWalletId"`
	DestAddress    string  `json:"destAddress"`
	Amount         string  `json:"amount"`
	NetworkFee     string  `json:"networkFee"`
	TxHash         string  `json:"txHash,omitempty"`
	Status         string  `json:"status"`
	StatusName     string  `json:"statusName"`
	Description    string  `json:"description"`
	Posted         bool    `json:"posted"`
	PostedVersion  int     `json:"postedVersion"`
	DeletionMark   bool    `json:"deletionMark"`
	Version        int     `json:"version"`

	// Resolved references
	Merchant *postgres.RefDisplay `json:"merchant,omitempty"`
	Token    *postgres.RefDisplay `json:"token,omitempty"`
}

// FromCryptoWithdrawal creates response DTO from domain entity.
func FromCryptoWithdrawal(w *crypto_withdrawal.CryptoWithdrawal, refs ...postgres.ResolvedRefs) *CryptoWithdrawalResponse {
	resp := &CryptoWithdrawalResponse{
		ID:             w.ID.String(),
		Number:         w.Number,
		Date:           w.Date.Format(time.RFC3339),
		MerchantID:     w.MerchantID.String(),
		TokenID:        w.TokenID.String(),
		SourceWalletID: w.SourceWalletID.String(),
		DestAddress:    w.DestAddress,
		Amount:         w.Amount.String(),
		NetworkFee:     w.NetworkFee.String(),
		TxHash:         w.TxHash,
		Status:         string(w.Status),
		StatusName:     string(w.Status),
		Description:    w.Description,
		Posted:         w.Posted,
		PostedVersion:  w.PostedVersion,
		DeletionMark:   w.DeletionMark,
		Version:        w.Version,
	}

	if len(refs) > 0 {
		merch := refs[0].Get(TableMerchants, w.MerchantID)
		resp.Merchant = &merch
		tok := refs[0].Get(TableTokens, w.TokenID)
		resp.Token = &tok
	}

	return resp
}

// CollectCryptoWithdrawalRefs collects FK references for batch resolution.
func CollectCryptoWithdrawalRefs(resolver *postgres.ReferenceResolver, w *crypto_withdrawal.CryptoWithdrawal) {
	resolver.Add(TableMerchants, w.MerchantID)
	resolver.Add(TableTokens, w.TokenID)
}
