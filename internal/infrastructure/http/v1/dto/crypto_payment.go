package dto

import (
	"time"

	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/infrastructure/storage/postgres"
)

// --- Response DTOs (CryptoPayment is read-only — created by chain watcher) ---

// CryptoPaymentResponse is the response body for a crypto payment.
type CryptoPaymentResponse struct {
	ID            string  `json:"id"`
	Number        string  `json:"number"`
	Date          string  `json:"date"`
	InvoiceID     string  `json:"invoiceId"`
	MerchantID    string  `json:"merchantId"`
	TokenID       string  `json:"tokenId"`
	WalletID      string  `json:"walletId"`
	TxHash        string  `json:"txHash"`
	FromAddress   string  `json:"fromAddress"`
	Amount        string  `json:"amount"`
	BlockNumber   int64   `json:"blockNumber"`
	Confirmations int     `json:"confirmations"`
	RequiredConfs int     `json:"requiredConfs"`
	Status        string  `json:"status"`
	StatusName    string  `json:"statusName"`
	NetworkFee    string  `json:"networkFee"`
	CommissionBP  int     `json:"commissionBp"`
	DetectedAt    string  `json:"detectedAt"`
	ConfirmedAt   *string `json:"confirmedAt,omitempty"`
	Posted        bool    `json:"posted"`
	PostedVersion int     `json:"postedVersion"`
	DeletionMark  bool    `json:"deletionMark"`
	Version       int     `json:"version"`

	// Resolved references
	Merchant *postgres.RefDisplay `json:"merchant,omitempty"`
	Token    *postgres.RefDisplay `json:"token,omitempty"`
	Wallet   *postgres.RefDisplay `json:"wallet,omitempty"`
}

// FromCryptoPayment creates response DTO from domain entity.
func FromCryptoPayment(p *crypto_payment.CryptoPayment, refs ...postgres.ResolvedRefs) *CryptoPaymentResponse {
	resp := &CryptoPaymentResponse{
		ID:            p.ID.String(),
		Number:        p.Number,
		Date:          p.Date.Format(time.RFC3339),
		InvoiceID:     p.InvoiceID.String(),
		MerchantID:    p.MerchantID.String(),
		TokenID:       p.TokenID.String(),
		WalletID:      p.WalletID.String(),
		TxHash:        p.TxHash,
		FromAddress:   p.FromAddress,
		Amount:        p.Amount.String(),
		BlockNumber:   p.BlockNumber,
		Confirmations: p.Confirmations,
		RequiredConfs: p.RequiredConfs,
		Status:        string(p.Status),
		StatusName:    string(p.Status),
		NetworkFee:    p.NetworkFee.String(),
		CommissionBP:  p.CommissionBP,
		DetectedAt:    p.DetectedAt.Format(time.RFC3339),
		Posted:        p.Posted,
		PostedVersion: p.PostedVersion,
		DeletionMark:  p.DeletionMark,
		Version:       p.Version,
	}

	if p.ConfirmedAt != nil {
		s := p.ConfirmedAt.Format(time.RFC3339)
		resp.ConfirmedAt = &s
	}

	if len(refs) > 0 {
		merch := refs[0].Get(TableMerchants, p.MerchantID)
		resp.Merchant = &merch
		tok := refs[0].Get(TableTokens, p.TokenID)
		resp.Token = &tok
		wlt := refs[0].Get(TableWallets, p.WalletID)
		resp.Wallet = &wlt
	}

	return resp
}

// CollectCryptoPaymentRefs collects FK references for batch resolution.
func CollectCryptoPaymentRefs(resolver *postgres.ReferenceResolver, p *crypto_payment.CryptoPayment) {
	resolver.Add(TableMerchants, p.MerchantID)
	resolver.Add(TableTokens, p.TokenID)
	resolver.Add(TableWallets, p.WalletID)
}

// --- Stub Create/Update DTOs for BaseDocumentHandler compliance ---
// CryptoPayment is read-only, but BaseDocumentHandler requires type params.

// CreateCryptoPaymentRequest is a minimal DTO (payments are created by chain watcher, not API).
type CreateCryptoPaymentRequest struct {
	// Intentionally empty — payments are not created via API
}

func (r *CreateCryptoPaymentRequest) ToEntity() *crypto_payment.CryptoPayment {
	return &crypto_payment.CryptoPayment{} // never used
}

// UpdateCryptoPaymentRequest is a minimal DTO (payments are not updated via API).
type UpdateCryptoPaymentRequest struct {
	Version int `json:"version" binding:"required"`
}

func (r *UpdateCryptoPaymentRequest) ApplyTo(p *crypto_payment.CryptoPayment) {
	p.Version = r.Version // never used
}

// ToEntity creates a minimal entity for type compliance.
func (r *CreateCryptoPaymentRequest) ToEntityFull(
	amount types.CryptoAmount,
) *crypto_payment.CryptoPayment {
	_ = amount // prevent unused warning
	return &crypto_payment.CryptoPayment{}
}
