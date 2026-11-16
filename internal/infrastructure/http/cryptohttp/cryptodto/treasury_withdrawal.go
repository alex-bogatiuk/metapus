package cryptodto

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/infrastructure/storage/postgres"
)

// CreateTreasuryWithdrawalRequest is the request body for creating a treasury withdrawal.
type CreateTreasuryWithdrawalRequest struct {
	TokenID        string          `json:"tokenId" binding:"required"`
	SourceWalletID string          `json:"sourceWalletId" binding:"required"`
	AddressID      string          `json:"addressId" binding:"required"`
	DestAddress    string          `json:"destAddress" binding:"required"`
	Amount         types.RawAmount `json:"amount" binding:"required"`
	Description    string          `json:"description,omitempty"`
}

func (r CreateTreasuryWithdrawalRequest) ToEntity() *treasury_withdrawal.TreasuryWithdrawal {
	tokenID, _ := id.Parse(r.TokenID)
	sourceWalletID, _ := id.Parse(r.SourceWalletID)
	addressID, _ := id.Parse(r.AddressID)
	doc := treasury_withdrawal.NewTreasuryWithdrawal(
		tokenID,
		sourceWalletID,
		addressID,
		r.DestAddress,
		r.Amount,
	)
	doc.Description = r.Description
	return doc
}

// CreateReceiptRefundRequest prepares the return of an unidentified receipt to
// its sender. Amount is what leaves suspense, in token minor units; with
// gasPayer "recipient" withheldAmount of it stays with the platform and the
// payer receives the rest.
type CreateReceiptRefundRequest struct {
	ReceiptID      string          `json:"receiptId" binding:"required,uuid"`
	SourceWalletID string          `json:"sourceWalletId" binding:"required,uuid"`
	DestAddress    string          `json:"destAddress" binding:"required"`
	Evidence       string          `json:"evidence" binding:"required,max=2000"`
	Reason         string          `json:"reason" binding:"required,max=2000"`
	Amount         types.RawAmount `json:"amount" binding:"required"`
	GasPayer       string          `json:"gasPayer" binding:"required,oneof=platform recipient"`
	WithheldAmount types.RawAmount `json:"withheldAmount"`
}

// UpdateTreasuryWithdrawalRequest updates editable fields before approval.
type UpdateTreasuryWithdrawalRequest struct {
	Description string `json:"description,omitempty"`
}

func (r UpdateTreasuryWithdrawalRequest) ApplyTo(w *treasury_withdrawal.TreasuryWithdrawal) {
	w.Description = r.Description
}

// TreasuryWithdrawalResponse is the response body for a treasury withdrawal.
type TreasuryWithdrawalResponse struct {
	Object         string               `json:"object"`
	ID             string               `json:"id"`
	Version        int                  `json:"version"`
	Number         string               `json:"number"`
	Date           string               `json:"date"`
	TokenID        string               `json:"tokenId"`
	SourceWalletID string               `json:"sourceWalletId"`
	AddressID      string               `json:"addressId"`
	DestAddress    string               `json:"destAddress"`
	Amount         string               `json:"amount"`
	NetworkFee     string               `json:"networkFee"`
	TxHash         string               `json:"txHash,omitempty"`
	Status         string               `json:"status"`
	ApprovedBy     string               `json:"approvedBy,omitempty"`
	ApprovedAt     string               `json:"approvedAt,omitempty"`
	Description    string               `json:"description,omitempty"`
	CreatedBy      string               `json:"createdBy,omitempty"`
	CreatedAt      string               `json:"createdAt"`
	UpdatedAt      string               `json:"updatedAt"`
	Token          *postgres.RefDisplay `json:"token,omitempty"`
	SourceWallet   *postgres.RefDisplay `json:"sourceWallet,omitempty"`
	// Purpose is "revenue" or "receipt_refund"; a refund names its receipt and
	// who bears the network fee.
	Purpose        string  `json:"purpose"`
	ReceiptID      *string `json:"receiptId,omitempty"`
	GasPayer       string  `json:"gasPayer"`
	WithheldAmount string  `json:"withheldAmount"`
}

func FromTreasuryWithdrawal(w *treasury_withdrawal.TreasuryWithdrawal, refs ...postgres.ResolvedRefs) *TreasuryWithdrawalResponse {
	resp := &TreasuryWithdrawalResponse{
		Object:         "treasury_withdrawal",
		ID:             w.ID.String(),
		Version:        w.Version,
		Number:         w.Number,
		Date:           w.Date.Format(time.RFC3339),
		TokenID:        w.TokenID.String(),
		SourceWalletID: w.SourceWalletID.String(),
		AddressID:      w.AddressID.String(),
		DestAddress:    w.DestAddress,
		Amount:         w.Amount.String(),
		NetworkFee:     w.NetworkFee.String(),
		TxHash:         w.TxHash,
		Status:         string(w.Status),
		Description:    w.Description,
		CreatedAt:      w.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      w.UpdatedAt.Format(time.RFC3339),
		Purpose:        string(w.Purpose),
		GasPayer:       string(w.GasPayer),
		WithheldAmount: w.WithheldAmount.String(),
	}
	if resp.Purpose == "" {
		resp.Purpose, resp.GasPayer = string(treasury_withdrawal.PurposeRevenue), string(treasury_withdrawal.GasPayerPlatform)
	}
	if w.ReceiptID != nil {
		receipt := w.ReceiptID.String()
		resp.ReceiptID = &receipt
	}
	if !id.IsNil(w.CreatedBy) {
		resp.CreatedBy = w.CreatedBy.String()
	}
	if w.ApprovedBy != nil && !id.IsNil(*w.ApprovedBy) {
		resp.ApprovedBy = w.ApprovedBy.String()
	}
	if w.ApprovedAt != nil {
		resp.ApprovedAt = w.ApprovedAt.Format(time.RFC3339)
	}
	if len(refs) > 0 {
		tok := refs[0].Get(TableTokens, w.TokenID)
		resp.Token = &tok
		wlt := refs[0].Get(TableWallets, w.SourceWalletID)
		resp.SourceWallet = &wlt
	}
	return resp
}

// CollectTreasuryWithdrawalRefs collects FK references for batch resolution.
func CollectTreasuryWithdrawalRefs(resolver *postgres.ReferenceResolver, w *treasury_withdrawal.TreasuryWithdrawal) {
	resolver.Add(TableTokens, w.TokenID)
	resolver.Add(TableWallets, w.SourceWalletID)
}
