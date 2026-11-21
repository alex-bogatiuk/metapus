// Package crypto_invoice provides the CryptoInvoice document.
// CryptoInvoice represents a payment request from a merchant to a customer.
// When posted, it records crypto balance movements for the assigned wallet.
package crypto_invoice

import (
	"context"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto/publicref"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// InvoiceStatus defines the lifecycle state of a crypto invoice.
type InvoiceStatus string
type CreationSource string
type InvoiceKind string

const (
	InvoiceStatusCreated       InvoiceStatus = "created"        // awaiting payment
	InvoiceStatusPartiallyPaid InvoiceStatus = "partially_paid" // partial payment received
	InvoiceStatusPaid          InvoiceStatus = "paid"           // full payment, awaiting confirmations
	InvoiceStatusConfirmed     InvoiceStatus = "confirmed"      // enough canonical funds reached chain finality
	InvoiceStatusCompleted     InvoiceStatus = "completed"      // enough finalized funds were credited to merchant ledger
	InvoiceStatusUnderpaid     InvoiceStatus = "underpaid"      // expired after receiving a positive insufficient amount
	InvoiceStatusExpired       InvoiceStatus = "expired"        // TTL expired without payment
	InvoiceStatusCancelled     InvoiceStatus = "cancelled"      // cancelled by merchant
	InvoiceStatusReversed      InvoiceStatus = "reversed"       // confirmed/completed invoice lost canonical funds

	CreationSourceMerchantAPI CreationSource = "merchant_api"
	CreationSourcePortal      CreationSource = "portal"
	CreationSourceSystem      CreationSource = "system"

	InvoiceKindPayment InvoiceKind = "payment"
	InvoiceKindTopUp   InvoiceKind = "top_up"
)

// IsValid reports whether s is a known invoice lifecycle status.
func (s InvoiceStatus) IsValid() bool {
	switch s {
	case InvoiceStatusCreated, InvoiceStatusPartiallyPaid, InvoiceStatusPaid,
		InvoiceStatusConfirmed, InvoiceStatusCompleted, InvoiceStatusUnderpaid,
		InvoiceStatusExpired, InvoiceStatusCancelled, InvoiceStatusReversed:
		return true
	default:
		return false
	}
}

const (
	MaxMetadataPairs = 50
	MaxMetadataKey   = 40
	MaxMetadataValue = 500
)

const DocumentType = "CryptoInvoice"

// CryptoInvoice represents a cryptocurrency payment request.
// It is a document that tracks expected vs received amounts for a merchant.
type CryptoInvoice struct {
	entity.Document

	// PublicRef is an opaque customer-facing checkout reference.
	PublicRef string `db:"public_ref" json:"publicRef" meta:"label:Публичная ссылка"`

	// Merchant that created the invoice
	MerchantID id.ID `db:"merchant_id" json:"merchantId" meta:"label:Мерчант,ref:merchant"`

	// Token to receive payment in
	TokenID id.ID `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`

	// Wallet assigned for receiving funds (set during creation)
	WalletID *id.ID `db:"wallet_id" json:"walletId,omitempty" meta:"label:Кошелёк,ref:wallet"`

	// Expected amount in token minor units (e.g., wei, satoshi)
	ExpectedAmount types.RawAmount `db:"expected_amount" json:"expectedAmount" meta:"label:Ожидаемая сумма"`

	// Received amount (updated by chain watcher)
	ReceivedAmount types.RawAmount `db:"received_amount" json:"receivedAmount" meta:"label:Полученная сумма"`

	// ConfirmedAmount is the sum of canonical payments that reached network finality.
	ConfirmedAmount types.RawAmount `db:"confirmed_amount" json:"confirmedAmount" meta:"label:Подтвержденная сумма"`

	// CreditedAmount is the sum of canonical finalized payments posted to the merchant ledger.
	CreditedAmount types.RawAmount `db:"credited_amount" json:"creditedAmount" meta:"label:Зачисленная сумма"`

	// Overpaid amount: received - expected, when received > expected (zero otherwise)
	OverpaidAmount types.RawAmount `db:"overpaid_amount" json:"overpaidAmount" meta:"label:Сумма переплаты"`

	// Invoice status (FSM)
	Status InvoiceStatus `db:"status" json:"status" meta:"label:Статус"`

	// SinglePayment is latched for the lifetime of this invoice. A merchant setting
	// change never reopens an invoice that has entered the single-payment mode.
	SinglePayment     bool   `db:"single_payment" json:"singlePayment"`
	SelectedPaymentID *id.ID `db:"selected_payment_id" json:"-"`

	// Expiration time
	ExpiresAt time.Time `db:"expires_at" json:"expiresAt" meta:"label:Истекает"`

	// NotificationURL overrides registered merchant endpoints for this invoice.
	NotificationURL string `db:"notification_url" json:"notificationUrl,omitempty" meta:"label:Notification URL"`

	// ReturnURL is a browser-only UX destination and never changes invoice state.
	ReturnURL string `db:"return_url" json:"returnUrl,omitempty" meta:"label:Return URL"`

	// Metadata is echoed back by the Merchant API and in every invoice webhook.
	Metadata map[string]string `db:"metadata" json:"metadata,omitempty" meta:"label:Метаданные"`

	// RequiredConfirmations and FinalityMode are immutable platform policy snapshots.
	RequiredConfirmations int    `db:"required_confirmations" json:"requiredConfirmations" meta:"label:Требуемые подтверждения"`
	FinalityMode          string `db:"finality_mode" json:"finalityMode" meta:"label:Режим финальности"`

	PaidAt      *time.Time `db:"paid_at" json:"paidAt,omitempty" meta:"label:Оплачен"`
	ConfirmedAt *time.Time `db:"confirmed_at" json:"confirmedAt,omitempty" meta:"label:Подтвержден"`
	CompletedAt *time.Time `db:"completed_at" json:"completedAt,omitempty" meta:"label:Завершен"`
	ReversedAt  *time.Time `db:"reversed_at" json:"reversedAt,omitempty" meta:"label:Отозван"`

	// Checkout view counters are internal analytics. They are deliberately
	// excluded from public invoice JSON and merchant webhook payloads.
	ViewCount     int64      `db:"view_count" json:"-"`
	FirstViewedAt *time.Time `db:"first_viewed_at" json:"-"`
	LastViewedAt  *time.Time `db:"last_viewed_at" json:"-"`

	// ExternalID is a merchant-owned business reference, not a transport key.
	ExternalID string `db:"external_id" json:"externalId,omitempty" meta:"label:Внешний ID"`

	// Merchant's order reference
	OrderID string `db:"order_id" json:"orderId,omitempty" meta:"label:Номер заказа"`

	// TTLMinutes preserves the validated request intent independently of ExpiresAt.
	TTLMinutes int `db:"ttl_minutes" json:"ttlMinutes" meta:"label:TTL, минут"`

	// Customer info
	CustomerID string `db:"customer_id" json:"customerId,omitempty" meta:"label:ID клиента"`

	// CreationSource separates merchant API, portal, and system-generated audit flows.
	CreationSource CreationSource `db:"creation_source" json:"creationSource" meta:"label:Источник создания"`

	// InvoiceKind is a platform-owned classification, independent of merchant references.
	InvoiceKind InvoiceKind `db:"invoice_kind" json:"-"`

	// APIKeyID is the merchant API key used to create this invoice.
	// NULL for invoices created via the ERP admin interface (JWT user context).
	// Audit chain: APIKeyID → cat_merchant_api_keys.CreatedByUserID → platform user.
	APIKeyID *id.ID `db:"api_key_id" json:"apiKeyId,omitempty" meta:"label:API-ключ"`
}

// AcceptsPayments reports whether the checkout should still present its address.
func (inv *CryptoInvoice) AcceptsPayments() bool {
	if inv.Status != InvoiceStatusCreated && inv.Status != InvoiceStatusPartiallyPaid {
		return false
	}
	return !inv.SinglePayment || inv.SelectedPaymentID == nil
}

// NewCryptoInvoice creates a new CryptoInvoice with required fields.
func NewCryptoInvoice(merchantID, tokenID id.ID, expectedAmount types.RawAmount) *CryptoInvoice {
	return &CryptoInvoice{
		Document:        entity.NewDocument(),
		PublicRef:       publicref.MustNewInvoice(),
		MerchantID:      merchantID,
		TokenID:         tokenID,
		ExpectedAmount:  expectedAmount,
		TTLMinutes:      60,
		ReceivedAmount:  types.ZeroRawAmount(),
		ConfirmedAmount: types.ZeroRawAmount(),
		CreditedAmount:  types.ZeroRawAmount(),
		OverpaidAmount:  types.ZeroRawAmount(),
		Metadata:        make(map[string]string),
		Status:          InvoiceStatusCreated,
		CreationSource:  CreationSourcePortal,
		InvoiceKind:     InvoiceKindPayment,
		ExpiresAt:       time.Now().Add(30 * time.Minute), // default 30 min TTL
	}
}

// Validate implements entity.Validatable — pure function, no DB calls.
func (inv *CryptoInvoice) Validate(ctx context.Context) error {
	if err := inv.Document.Validate(ctx); err != nil {
		return err
	}
	if inv.InvoiceKind != InvoiceKindPayment && inv.InvoiceKind != InvoiceKindTopUp {
		return apperror.NewValidation("invalid invoice kind").WithDetail("field", "invoiceKind")
	}
	if inv.InvoiceKind == InvoiceKindTopUp && inv.CreationSource != CreationSourceSystem {
		return apperror.NewValidation("top-up invoices must be system-generated").WithDetail("field", "creationSource")
	}
	if inv.OrderID != "" {
		if err := ValidateOrderReference(inv.OrderID); err != nil {
			return err
		}
	}
	if inv.ExternalID != "" {
		if err := ValidateExternalReference(inv.ExternalID); err != nil {
			return err
		}
	}

	if err := publicref.Validate(inv.PublicRef); err != nil {
		return apperror.NewValidation("invalid public checkout reference").
			WithDetail("field", "publicRef")
	}

	if id.IsNil(inv.MerchantID) {
		return apperror.NewValidation("merchant is required").
			WithDetail("field", "merchantId")
	}

	if id.IsNil(inv.TokenID) {
		return apperror.NewValidation("token is required").
			WithDetail("field", "tokenId")
	}

	if !inv.ExpectedAmount.IsPositive() {
		return apperror.NewValidation("expected amount must be positive").
			WithDetail("field", "expectedAmount")
	}
	zero := types.ZeroRawAmount()
	if inv.ReceivedAmount.Cmp(zero) < 0 || inv.ConfirmedAmount.Cmp(zero) < 0 || inv.CreditedAmount.Cmp(zero) < 0 || inv.OverpaidAmount.Cmp(zero) < 0 {
		return apperror.NewValidation("invoice amounts must be non-negative").
			WithDetail("field", "amounts")
	}
	if inv.ConfirmedAmount.Cmp(inv.ReceivedAmount) > 0 {
		return apperror.NewValidation("confirmed amount cannot exceed received amount").
			WithDetail("field", "confirmedAmount")
	}
	if inv.CreditedAmount.Cmp(inv.ConfirmedAmount) > 0 {
		return apperror.NewValidation("credited amount cannot exceed confirmed amount").
			WithDetail("field", "creditedAmount")
	}
	return ValidateMetadata(inv.Metadata)
}

// --- RLSDimensionable override ---

// GetRLSDimensions returns merchant dimension for RLS filtering.
func (inv *CryptoInvoice) GetRLSDimensions() map[string]string {
	return map[string]string{
		"merchant": inv.MerchantID.String(),
	}
}

// GetDocumentType returns the document type name.
func (inv *CryptoInvoice) GetDocumentType() string { return DocumentType }

// NOTE: CryptoInvoice does NOT implement posting.Postable or any MovementSource.
// Invoice is a payment REQUEST, not a business fact. Register movements are
// generated by CryptoPayment when the blockchain transaction is confirmed.
// This avoids double-counting: Invoice receipt + Payment receipt on the same wallet.
