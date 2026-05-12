package dto

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/storage/postgres"
)

// --- Request DTOs ---

// CreateCryptoInvoiceRequest is the request body for creating a crypto invoice.
type CreateCryptoInvoiceRequest struct {
	Number         string    `json:"number,omitempty"`
	Date           time.Time `json:"date" binding:"required"`
	MerchantID     string    `json:"merchantId" binding:"required"`
	TokenID        string    `json:"tokenId" binding:"required"`
	ExpectedAmount string    `json:"expectedAmount" binding:"required"` // string for CryptoAmount
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	CallbackURL    string    `json:"callbackUrl,omitempty"`
	ExternalID     string    `json:"externalId,omitempty"`
	OrderID        string    `json:"orderId,omitempty"`
	CustomerEmail  string    `json:"customerEmail,omitempty"`
	Description    string    `json:"description,omitempty"`
	PostImmediately bool    `json:"postImmediately,omitempty"`
}

// ToEntity converts request to domain entity.
func (r *CreateCryptoInvoiceRequest) ToEntity() *crypto_invoice.CryptoInvoice {
	merchantID, _ := id.Parse(r.MerchantID)
	tokenID, _ := id.Parse(r.TokenID)
	expectedAmount, _ := types.NewCryptoAmountFromString(r.ExpectedAmount)

	doc := crypto_invoice.NewCryptoInvoice(merchantID, tokenID, expectedAmount)
	doc.Number = r.Number
	doc.Date = r.Date
	doc.CallbackURL = r.CallbackURL
	doc.ExternalID = r.ExternalID
	doc.OrderID = r.OrderID
	doc.CustomerEmail = r.CustomerEmail
	doc.Description = r.Description

	if r.ExpiresAt != nil {
		doc.ExpiresAt = *r.ExpiresAt
	}

	return doc
}

// UpdateCryptoInvoiceRequest is the request body for updating a crypto invoice.
type UpdateCryptoInvoiceRequest struct {
	Version        int        `json:"version" binding:"required,min=1"`
	Number         *string    `json:"number,omitempty"`
	Date           *time.Time `json:"date,omitempty"`
	MerchantID     *string    `json:"merchantId,omitempty"`
	TokenID        *string    `json:"tokenId,omitempty"`
	ExpectedAmount *string    `json:"expectedAmount,omitempty"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	CallbackURL    *string    `json:"callbackUrl,omitempty"`
	ExternalID     *string    `json:"externalId,omitempty"`
	OrderID        *string    `json:"orderId,omitempty"`
	CustomerEmail  *string    `json:"customerEmail,omitempty"`
	Description    *string    `json:"description,omitempty"`
}

// ApplyTo applies updates to an existing entity.
func (r *UpdateCryptoInvoiceRequest) ApplyTo(doc *crypto_invoice.CryptoInvoice) {
	doc.SetVersion(r.Version)
	if r.Number != nil {
		doc.Number = *r.Number
	}
	if r.Date != nil {
		doc.Date = *r.Date
	}
	if r.MerchantID != nil {
		merchantID, _ := id.Parse(*r.MerchantID)
		doc.MerchantID = merchantID
	}
	if r.TokenID != nil {
		tokenID, _ := id.Parse(*r.TokenID)
		doc.TokenID = tokenID
	}
	if r.ExpectedAmount != nil {
		amount, _ := types.NewCryptoAmountFromString(*r.ExpectedAmount)
		doc.ExpectedAmount = amount
	}
	if r.ExpiresAt != nil {
		doc.ExpiresAt = *r.ExpiresAt
	}
	if r.CallbackURL != nil {
		doc.CallbackURL = *r.CallbackURL
	}
	if r.ExternalID != nil {
		doc.ExternalID = *r.ExternalID
	}
	if r.OrderID != nil {
		doc.OrderID = *r.OrderID
	}
	if r.CustomerEmail != nil {
		doc.CustomerEmail = *r.CustomerEmail
	}
	if r.Description != nil {
		doc.Description = *r.Description
	}
}

// --- Response DTOs ---

// CryptoInvoiceResponse represents a crypto invoice in API responses.
type CryptoInvoiceResponse struct {
	ID             string                         `json:"id"`
	Number         string                         `json:"number"`
	Date           time.Time                      `json:"date"`
	Posted         bool                           `json:"posted"`
	PostedVersion  int                            `json:"postedVersion,omitempty"`
	MerchantID     string                         `json:"merchantId"`
	TokenID        string                         `json:"tokenId"`
	WalletID       *string                        `json:"walletId,omitempty"`
	ExpectedAmount string                         `json:"expectedAmount"` // string for precision
	ReceivedAmount string                         `json:"receivedAmount"`
	OverpaidAmount string                         `json:"overpaidAmount"`
	Status         string                         `json:"status"`
	StatusName     string                         `json:"statusName"`
	ExpiresAt      time.Time                      `json:"expiresAt"`
	CallbackURL    string                         `json:"callbackUrl,omitempty"`
	ExternalID     string                         `json:"externalId,omitempty"`
	OrderID        string                         `json:"orderId,omitempty"`
	CustomerEmail  string                         `json:"customerEmail,omitempty"`
	Description    string                         `json:"description,omitempty"`
	Version        int                            `json:"version"`
	DeletionMark   bool                           `json:"deletionMark"`
	Attributes     entity.Attributes              `json:"attributes,omitempty"`
	CreatedAt      time.Time                      `json:"createdAt"`
	UpdatedAt      time.Time                      `json:"updatedAt"`

	// Resolved references
	Merchant     *postgres.RefDisplay `json:"merchant,omitempty"`
	Token        *postgres.RefDisplay `json:"token,omitempty"`
	Wallet       *postgres.RefDisplay `json:"wallet,omitempty"`
}


// FromCryptoInvoice converts domain entity to response DTO.
func FromCryptoInvoice(doc *crypto_invoice.CryptoInvoice, refs postgres.ResolvedRefs) *CryptoInvoiceResponse {
	resp := &CryptoInvoiceResponse{
		ID:             doc.ID.String(),
		Number:         doc.Number,
		Date:           doc.Date,
		Posted:         doc.Posted,
		PostedVersion:  doc.PostedVersion,
		MerchantID:     doc.MerchantID.String(),
		TokenID:        doc.TokenID.String(),
		ExpectedAmount: doc.ExpectedAmount.String(),
		ReceivedAmount: doc.ReceivedAmount.String(),
		OverpaidAmount: doc.OverpaidAmount.String(),
		Status:         string(doc.Status),
		StatusName:     string(doc.Status),
		ExpiresAt:      doc.ExpiresAt,
		CallbackURL:    doc.CallbackURL,
		ExternalID:     doc.ExternalID,
		OrderID:        doc.OrderID,
		CustomerEmail:  doc.CustomerEmail,
		Description:    doc.Description,
		Version:        doc.Version,
		DeletionMark:   doc.DeletionMark,
		Attributes:     doc.Attributes,
		CreatedAt:      doc.CreatedAt,
		UpdatedAt:      doc.UpdatedAt,
	}

	if doc.WalletID != nil {
		s := doc.WalletID.String()
		resp.WalletID = &s
	}

	// Resolved references
	if refs != nil {
		merch := refs.Get(TableMerchants, doc.MerchantID)
		resp.Merchant = &merch
		tok := refs.Get(TableTokens, doc.TokenID)
		resp.Token = &tok
		resp.Wallet = refs.GetPtr(TableWallets, doc.WalletID)
	}

	return resp
}

// CollectCryptoInvoiceRefs registers reference IDs for batch resolution.
func CollectCryptoInvoiceRefs(resolver *postgres.ReferenceResolver, doc *crypto_invoice.CryptoInvoice) {
	resolver.Add(TableMerchants, doc.MerchantID)
	resolver.Add(TableTokens, doc.TokenID)
	resolver.AddPtr(TableWallets, doc.WalletID)
}
