package dto

import (
	"time"

	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/documents/crypto_invoice"
)

// ─────────────────────────────────────────────────────────────────
// Merchant Public API (/merchant/v1/) DTOs
//
// These types are the external contract for merchant integrations.
// They intentionally expose less than internal ERP DTOs:
//   - No internal document IDs beyond invoice ID
//   - No posting/version semantics
//   - Amounts as human-readable decimal strings
// ─────────────────────────────────────────────────────────────────

// CreateMerchantInvoiceRequest is POSTed by merchants to create an invoice.
type CreateMerchantInvoiceRequest struct {
	// Amount in token minor units (e.g. 10_500_000 for 10.5 USDT with 6 decimal places). Required.
	// Use the token's decimalPlaces to convert: amount = humanAmount × 10^decimalPlaces.
	Amount int64 `json:"amount" binding:"required"`

	// Currency code matching a configured token, e.g. "USDT_TRC20". Required.
	Currency string `json:"currency" binding:"required"`

	// OrderID is the merchant's own idempotency key (Bender pattern).
	// If a duplicate OrderID is received, the existing invoice is returned.
	OrderID *string `json:"orderId"`

	// Description is a free-text label shown on the payment page.
	Description *string `json:"description"`

	// TTLMinutes overrides the default expiry window (default: 60, range: 5–1440).
	TTLMinutes *int `json:"ttlMinutes"`

	// CallbackURL overrides the merchant's default webhook URL for this invoice.
	CallbackURL *string `json:"callbackUrl"`

	// CustomerEmail is optional metadata to associate with the payer.
	CustomerEmail *string `json:"customerEmail"`
}

// MerchantInvoiceResponse is the response for invoice create/get.
type MerchantInvoiceResponse struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	Amount        int64   `json:"amount"`        // minor units (same as request)
	Currency      string  `json:"currency"`      // token code, e.g. "USDT_TRC20"
	Network       string  `json:"network"`       // e.g. "TRON"
	WalletAddress string  `json:"walletAddress"` // address to pay to
	ExpiresAt     string  `json:"expiresAt"`     // RFC3339
	OrderID       *string `json:"orderId,omitempty"`
	Description   *string `json:"description,omitempty"`
	CreatedAt     string  `json:"createdAt"` // RFC3339
}

// MerchantInvoiceFromEntity maps CryptoInvoice to the merchant-facing response.
// walletAddress and currency/network are resolved by the handler.
func MerchantInvoiceFromEntity(inv *crypto_invoice.CryptoInvoice, walletAddress, currency, network string) MerchantInvoiceResponse {
	resp := MerchantInvoiceResponse{
		ID:            inv.ID.String(),
		Status:        string(inv.Status),
		Amount:        inv.ExpectedAmount.Int64(),
		Currency:      currency,
		Network:       network,
		WalletAddress: walletAddress,
		ExpiresAt:     inv.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt:     inv.CreatedAt.UTC().Format(time.RFC3339),
	}
	if inv.OrderID != "" {
		s := inv.OrderID
		resp.OrderID = &s
	}
	if inv.Description != "" {
		s := inv.Description
		resp.Description = &s
	}
	return resp
}

// ─────────────────────────────────────────────────────────────────
// API Key Management DTOs (used by admin/internal endpoints on
// the main /api/v1/ route for managing keys per merchant)
// ─────────────────────────────────────────────────────────────────

// CreateMerchantAPIKeyRequest creates a new API key for a merchant.
type CreateMerchantAPIKeyRequest struct {
	// Name is a human-readable label, e.g. "Production Key".
	Name string `json:"name" binding:"required"`

	// Scopes defaults to ["invoice:create","invoice:read"] if omitted.
	Scopes []string `json:"scopes"`

	// ExpiresAt is optional; if omitted the key never expires.
	ExpiresAt *time.Time `json:"expiresAt"`
}

// MerchantAPIKeyResponse is returned when a new key is created.
// Plaintext is set only once and is never stored or returned again.
type MerchantAPIKeyResponse struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	KeyPrefix       string   `json:"keyPrefix"`
	Scopes          []string `json:"scopes"`
	IsActive        bool     `json:"isActive"`
	CreatedByUserID *string  `json:"createdByUserId,omitempty"`
	ExpiresAt       *string  `json:"expiresAt,omitempty"`
	CreatedAt       string   `json:"createdAt"`
	Plaintext       *string  `json:"plaintext,omitempty"` // set only on creation
}

// MerchantAPIKeyListItem is a list entry (no Plaintext).
type MerchantAPIKeyListItem struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	KeyPrefix       string   `json:"keyPrefix"`
	Scopes          []string `json:"scopes"`
	IsActive        bool     `json:"isActive"`
	CreatedByUserID *string  `json:"createdByUserId,omitempty"`
	LastUsedAt      *string  `json:"lastUsedAt,omitempty"`
	ExpiresAt       *string  `json:"expiresAt,omitempty"`
	CreatedAt       string   `json:"createdAt"`
}

// MerchantAPIKeyToListItem maps domain key to list response (no hash exposed).
func MerchantAPIKeyToListItem(key *merchant.MerchantAPIKey) MerchantAPIKeyListItem {
	scopes := make([]string, len(key.Scopes))
	for i, s := range key.Scopes {
		scopes[i] = string(s)
	}
	item := MerchantAPIKeyListItem{
		ID:        key.ID.String(),
		Name:      key.Name,
		KeyPrefix: key.KeyPrefix,
		Scopes:    scopes,
		IsActive:  key.IsActive,
		CreatedAt: key.CreatedAt.UTC().Format(time.RFC3339),
	}
	if key.CreatedByUserID != nil {
		s := key.CreatedByUserID.String()
		item.CreatedByUserID = &s
	}
	if key.LastUsedAt != nil {
		s := key.LastUsedAt.UTC().Format(time.RFC3339)
		item.LastUsedAt = &s
	}
	if key.ExpiresAt != nil {
		s := key.ExpiresAt.UTC().Format(time.RFC3339)
		item.ExpiresAt = &s
	}
	return item
}

// MerchantAPIKeyToCreateResponse maps a newly generated key + plaintext to the creation response.
func MerchantAPIKeyToCreateResponse(key *merchant.MerchantAPIKey, plaintext string) MerchantAPIKeyResponse {
	scopes := make([]string, len(key.Scopes))
	for i, s := range key.Scopes {
		scopes[i] = string(s)
	}
	resp := MerchantAPIKeyResponse{
		ID:        key.ID.String(),
		Name:      key.Name,
		KeyPrefix: key.KeyPrefix,
		Scopes:    scopes,
		IsActive:  key.IsActive,
		CreatedAt: key.CreatedAt.UTC().Format(time.RFC3339),
		Plaintext: &plaintext,
	}
	if key.CreatedByUserID != nil {
		s := key.CreatedByUserID.String()
		resp.CreatedByUserID = &s
	}
	if key.ExpiresAt != nil {
		s := key.ExpiresAt.UTC().Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	return resp
}

// ─────────────────────────────────────────────────────────────────
// Merchant User DTOs (for /api/v1/merchant-admin/merchants/:id/users)
// ─────────────────────────────────────────────────────────────────

// MerchantUserItem represents a user's association with a merchant.
type MerchantUserItem struct {
	UserID     string `json:"userId"`
	MerchantID string `json:"merchantId"`
	// Role: 1=Owner, 2=Manager, 3=Viewer. Extensible — add roles in domain, no DTO change needed.
	Role         int    `json:"role"`
	RoleName     string `json:"roleName"`     // human-readable role name
	CreatedAt    string `json:"createdAt"`    // RFC3339
	UserEmail    string `json:"userEmail,omitempty"`
	UserFullName string `json:"userFullName,omitempty"`
}

// AddMerchantUserRequest grants a user access to a merchant.
type AddMerchantUserRequest struct {
	UserID string `json:"userId" binding:"required"`
	Role   int    `json:"role"   binding:"required"`
}

// UpdateMerchantUserRoleRequest changes a user's role within a merchant.
type UpdateMerchantUserRoleRequest struct {
	Role int `json:"role" binding:"required"`
}

// MerchantUserFromDomain maps a domain MerchantUser to the DTO.
func MerchantUserFromDomain(u merchant.MerchantUser) MerchantUserItem {
	return MerchantUserItem{
		UserID:       u.UserID.String(),
		MerchantID:   u.MerchantID.String(),
		Role:         int(u.Role),
		RoleName:     u.Role.String(),
		CreatedAt:    u.CreatedAt.UTC().Format(time.RFC3339),
		UserEmail:    u.UserEmail,
		UserFullName: u.UserFullName,
	}
}
