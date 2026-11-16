package cryptodto

import (
	"time"

	"metapus/internal/core/types"
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
//   - Amounts as token minor units
// ─────────────────────────────────────────────────────────────────

// CreateMerchantInvoiceRequest is POSTed by merchants to create an invoice.
type CreateMerchantInvoiceRequest struct {
	// Amount in token minor units (e.g. 10_500_000 for 10.5 USDT with 6 decimal places). Required.
	// Use the token's decimalPlaces to convert: amount = humanAmount × 10^decimalPlaces.
	Amount types.RawAmount `json:"amount" binding:"required"`

	// Currency code matching a configured token, e.g. "USDT-TRC20". Required.
	Currency string `json:"currency" binding:"required"`

	// OrderID is the merchant's unique business order reference.
	OrderID *string `json:"orderId"`

	// ExternalID is another optional merchant business reference. It is not an
	// Idempotency-Key and must remain stable for the underlying business object.
	ExternalID *string `json:"externalId"`

	// Description is a free-text label shown on the payment page.
	Description *string `json:"description"`

	// TTLMinutes overrides the merchant default expiry window (range: 5–10080).
	TTLMinutes *int `json:"ttlMinutes"`

	// NotificationURL overrides registered merchant endpoints for this invoice.
	NotificationURL *string `json:"notificationUrl"`

	// ReturnURL is a browser-only UX destination and is never authoritative.
	ReturnURL *string `json:"returnUrl"`

	// Metadata is echoed in GET responses and invoice webhook events.
	Metadata map[string]string `json:"metadata"`

	// CustomerID is an optional merchant customer identifier (e.g. UUID, user ID, email).
	CustomerID *string `json:"customerId"`
}

// MerchantInvoiceResponse is the response for invoice create/get/list.
type MerchantInvoiceResponse struct {
	Object                string                    `json:"object"`
	ID                    string                    `json:"id"`
	CheckoutRef           string                    `json:"checkoutRef"`
	PayURL                string                    `json:"payUrl"`
	Status                string                    `json:"status"`
	AcceptsPayments       bool                      `json:"acceptsPayments"`
	ResourceVersion       int                       `json:"resourceVersion"`
	ExpectedAmount        string                    `json:"expectedAmount"`
	ReceivedAmount        string                    `json:"receivedAmount"`
	ConfirmedAmount       string                    `json:"confirmedAmount"`
	FeeAmount             string                    `json:"feeAmount"`
	CreditedAmount        string                    `json:"creditedAmount"`
	NetAmount             string                    `json:"netAmount"`
	OverpaidAmount        string                    `json:"overpaidAmount"`
	Currency              string                    `json:"currency"`
	Network               string                    `json:"network"`
	DecimalPlaces         int                       `json:"decimalPlaces"`
	WalletAddress         string                    `json:"walletAddress"`
	RequiredConfirmations int                       `json:"requiredConfirmations"`
	FinalityMode          string                    `json:"finalityMode"`
	ExpiresAt             string                    `json:"expiresAt"`
	OrderID               *string                   `json:"orderId,omitempty"`
	ExternalID            *string                   `json:"externalId,omitempty"`
	CustomerID            *string                   `json:"customerId,omitempty"`
	Description           *string                   `json:"description,omitempty"`
	NotificationURL       *string                   `json:"notificationUrl,omitempty"`
	ReturnURL             *string                   `json:"returnUrl,omitempty"`
	Metadata              map[string]string         `json:"metadata"`
	PaidAt                *string                   `json:"paidAt,omitempty"`
	ConfirmedAt           *string                   `json:"confirmedAt,omitempty"`
	CompletedAt           *string                   `json:"completedAt,omitempty"`
	ReversedAt            *string                   `json:"reversedAt,omitempty"`
	CreatedAt             string                    `json:"createdAt"`
	AsOf                  string                    `json:"asOf"`
	ChainObservedAt       *string                   `json:"chainObservedAt,omitempty"`
	Payments              []MerchantPaymentResponse `json:"payments"`
}

// MerchantInvoiceListResponse is returned by GET /merchant/v1/invoices.
type MerchantInvoiceListResponse struct {
	Object     string                    `json:"object"`
	Items      []MerchantInvoiceResponse `json:"items"`
	HasMore    bool                      `json:"hasMore"`
	NextCursor *string                   `json:"nextCursor,omitempty"`
}

// MerchantCurrencyItem describes an active token available in the Merchant Public API.
type MerchantCurrencyItem struct {
	Code                  string `json:"code"`
	Symbol                string `json:"symbol"`
	Name                  string `json:"name"`
	Network               string `json:"network"`
	NetworkName           string `json:"networkName"`
	Standard              string `json:"standard"`
	DecimalPlaces         int    `json:"decimalPlaces"`
	ContractAddress       string `json:"contractAddress,omitempty"`
	RequiredConfirmations int    `json:"requiredConfirmations"`
	FinalityMode          string `json:"finalityMode"`
	AcceptsPayments       bool   `json:"acceptsPayments"`
}

// MerchantCurrencyListResponse is returned by GET /merchant/v1/currencies.
type MerchantCurrencyListResponse struct {
	Items []MerchantCurrencyItem `json:"items"`
}

type MerchantPaymentResponse struct {
	ID        string `json:"id"`
	TxHash    string `json:"txHash"`
	EventKind string `json:"eventKind"`
	// LogIndex is the event index for the specified eventKind, retained for compatibility.
	LogIndex              int64   `json:"logIndex"`
	Amount                string  `json:"amount"`
	FeeAmount             string  `json:"feeAmount"`
	NetAmount             string  `json:"netAmount"`
	Status                string  `json:"status"`
	Canonicality          string  `json:"canonicality"`
	FinalityStatus        string  `json:"finalityStatus"`
	Confirmations         int     `json:"confirmations"`
	RequiredConfirmations int     `json:"requiredConfirmations"`
	SourceNonce           *int64  `json:"sourceNonce,omitempty"`
	SourceAddress         string  `json:"sourceAddress,omitempty"`
	ReplacementTxHash     *string `json:"replacementTxHash,omitempty"`
	DetectedAt            string  `json:"detectedAt"`
	ConfirmedAt           *string `json:"confirmedAt,omitempty"`
}

// MerchantInvoiceFromEntity maps CryptoInvoice to the merchant-facing response.
// walletAddress, currency/network, and decimalPlaces are resolved by the handler.
func MerchantInvoiceFromEntity(inv *crypto_invoice.CryptoInvoice, walletAddress, currency, network string, decimalPlaces int, payURL string) MerchantInvoiceResponse {
	resp := MerchantInvoiceResponse{
		Object:                "crypto_invoice",
		ID:                    inv.ID.String(),
		CheckoutRef:           inv.PublicRef,
		PayURL:                payURL,
		Status:                string(inv.Status),
		AcceptsPayments:       inv.AcceptsPayments(),
		ResourceVersion:       inv.Version,
		ExpectedAmount:        inv.ExpectedAmount.String(),
		ReceivedAmount:        inv.ReceivedAmount.String(),
		ConfirmedAmount:       inv.ConfirmedAmount.String(),
		FeeAmount:             "0",
		CreditedAmount:        inv.CreditedAmount.String(),
		NetAmount:             "0",
		OverpaidAmount:        inv.OverpaidAmount.String(),
		Currency:              currency,
		Network:               network,
		DecimalPlaces:         decimalPlaces,
		WalletAddress:         walletAddress,
		RequiredConfirmations: inv.RequiredConfirmations,
		FinalityMode:          inv.FinalityMode,
		ExpiresAt:             inv.ExpiresAt.UTC().Format(time.RFC3339),
		Metadata:              inv.Metadata,
		CreatedAt:             inv.CreatedAt.UTC().Format(time.RFC3339),
		AsOf:                  time.Now().UTC().Format(time.RFC3339Nano),
		Payments:              []MerchantPaymentResponse{},
	}
	if inv.OrderID != "" {
		s := inv.OrderID
		resp.OrderID = &s
	}
	if inv.ExternalID != "" {
		s := inv.ExternalID
		resp.ExternalID = &s
	}
	if inv.CustomerID != "" {
		s := inv.CustomerID
		resp.CustomerID = &s
	}
	if inv.Description != "" {
		s := inv.Description
		resp.Description = &s
	}
	if inv.NotificationURL != "" {
		s := inv.NotificationURL
		resp.NotificationURL = &s
	}
	if inv.ReturnURL != "" {
		s := inv.ReturnURL
		resp.ReturnURL = &s
	}
	setTime := func(value *time.Time) *string {
		if value == nil {
			return nil
		}
		formatted := value.UTC().Format(time.RFC3339Nano)
		return &formatted
	}
	resp.PaidAt = setTime(inv.PaidAt)
	resp.ConfirmedAt = setTime(inv.ConfirmedAt)
	resp.CompletedAt = setTime(inv.CompletedAt)
	resp.ReversedAt = setTime(inv.ReversedAt)
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
	RoleName     string `json:"roleName"`  // human-readable role name
	CreatedAt    string `json:"createdAt"` // RFC3339
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
