// Package crypto_invoice provides the CryptoInvoice document.
// CryptoInvoice represents a payment request from a merchant to a customer.
// When posted, it records crypto balance movements for the assigned wallet.
package crypto_invoice

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

// InvoiceStatus defines the lifecycle state of a crypto invoice.
type InvoiceStatus string

const (
	InvoiceStatusCreated      InvoiceStatus = "created"        // awaiting payment
	InvoiceStatusPartiallyPaid InvoiceStatus = "partially_paid" // partial payment received
	InvoiceStatusPaid         InvoiceStatus = "paid"           // full payment, awaiting confirmations
	InvoiceStatusOverpaid     InvoiceStatus = "overpaid"       // received > expected
	InvoiceStatusConfirmed    InvoiceStatus = "confirmed"      // confirmed, funds credited
	InvoiceStatusExpired      InvoiceStatus = "expired"        // TTL expired without payment
	InvoiceStatusCancelled    InvoiceStatus = "cancelled"      // cancelled by merchant
)

// CryptoInvoice represents a cryptocurrency payment request.
// It is a document that tracks expected vs received amounts for a merchant.
type CryptoInvoice struct {
	entity.Document

	// Merchant that created the invoice
	MerchantID id.ID `db:"merchant_id" json:"merchantId" meta:"label:Мерчант,ref:merchant"`

	// Token to receive payment in
	TokenID id.ID `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`

	// Wallet assigned for receiving funds (set during creation)
	WalletID *id.ID `db:"wallet_id" json:"walletId,omitempty" meta:"label:Кошелёк,ref:wallet"`

	// Expected amount in token minor units (e.g., wei, satoshi)
	ExpectedAmount types.CryptoAmount `db:"expected_amount" json:"expectedAmount" meta:"label:Ожидаемая сумма"`

	// Received amount (updated by chain watcher)
	ReceivedAmount types.CryptoAmount `db:"received_amount" json:"receivedAmount" meta:"label:Полученная сумма"`

	// Overpaid amount: received - expected, when received > expected (zero otherwise)
	OverpaidAmount types.CryptoAmount `db:"overpaid_amount" json:"overpaidAmount" meta:"label:Сумма переплаты"`

	// Invoice status (FSM)
	Status InvoiceStatus `db:"status" json:"status" meta:"label:Статус"`

	// Expiration time
	ExpiresAt time.Time `db:"expires_at" json:"expiresAt" meta:"label:Истекает"`

	// Merchant callback URL for status notifications
	CallbackURL string `db:"callback_url" json:"callbackUrl,omitempty" meta:"label:Callback URL"`

	// External idempotency key (Bender pattern)
	ExternalID string `db:"external_id" json:"externalId,omitempty" meta:"label:Внешний ID"`

	// Merchant's order reference
	OrderID string `db:"order_id" json:"orderId,omitempty" meta:"label:Номер заказа"`

	// Customer info
	CustomerEmail string `db:"customer_email" json:"customerEmail,omitempty" meta:"label:Email клиента"`

	// Lines (detailed breakdown — optional for simple invoices)
	Lines []CryptoInvoiceLine `db:"-" json:"lines" meta:"label:Позиции"`
}

// CryptoInvoiceLine represents a line item in the invoice.
type CryptoInvoiceLine struct {
	LineID      id.ID              `db:"line_id" json:"lineId"`
	LineNo      int                `db:"line_no" json:"lineNo" meta:"label:№"`
	Description string             `db:"description" json:"description" meta:"label:Описание"`
	Amount      types.CryptoAmount `db:"amount" json:"amount" meta:"label:Сумма"`
}

// NewCryptoInvoice creates a new CryptoInvoice with required fields.
func NewCryptoInvoice(merchantID, tokenID id.ID, expectedAmount types.CryptoAmount) *CryptoInvoice {
	return &CryptoInvoice{
		Document:       entity.NewDocument(),
		MerchantID:     merchantID,
		TokenID:        tokenID,
		ExpectedAmount: expectedAmount,
		ReceivedAmount: types.ZeroCryptoAmount(),
		OverpaidAmount: types.ZeroCryptoAmount(),
		Status:         InvoiceStatusCreated,
		ExpiresAt:      time.Now().Add(30 * time.Minute), // default 30 min TTL
		Lines:          make([]CryptoInvoiceLine, 0),
	}
}

// Validate implements entity.Validatable — pure function, no DB calls.
func (inv *CryptoInvoice) Validate(ctx context.Context) error {
	if err := inv.Document.Validate(ctx); err != nil {
		return err
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

	return nil
}

// --- LinesAccessor implementation ---

func (inv *CryptoInvoice) GetLines() []CryptoInvoiceLine {
	out := make([]CryptoInvoiceLine, len(inv.Lines))
	copy(out, inv.Lines)
	return out
}

func (inv *CryptoInvoice) SetLines(lines []CryptoInvoiceLine) {
	inv.Lines = make([]CryptoInvoiceLine, len(lines))
	copy(inv.Lines, lines)
}

// --- CurrencyAwareDoc stubs (crypto invoice uses TokenID, not CurrencyID) ---
// These satisfy the domain.CurrencyAwareDoc interface. CryptoInvoice does NOT
// use fiat CurrencyID — currency resolution is a no-op.

func (inv *CryptoInvoice) GetCurrencyID() id.ID          { return id.ID{} }
func (inv *CryptoInvoice) SetCurrencyID(_ id.ID)          {}
func (inv *CryptoInvoice) ValidateCurrency(_ context.Context) error { return nil }
func (inv *CryptoInvoice) GetContractID() *id.ID          { return nil }

// --- RLSDimensionable override ---

// GetRLSDimensions returns merchant dimension for RLS filtering.
func (inv *CryptoInvoice) GetRLSDimensions() map[string]string {
	return map[string]string{
		"merchant": inv.MerchantID.String(),
	}
}

// --- Postable interface ---

func (inv *CryptoInvoice) GetDocumentType() string { return "CryptoInvoice" }

// GenerateCryptoBalanceMovements implements posting.CryptoBalanceMovementSource.
// When posted (confirmed), creates a RECEIPT movement for the wallet.
func (inv *CryptoInvoice) GenerateCryptoBalanceMovements(ctx context.Context) ([]entity.CryptoBalanceMovement, error) {
	if inv.WalletID == nil || inv.ReceivedAmount.IsZero() {
		return nil, nil
	}

	newVersion := inv.PostedVersion + 1

	movement := entity.NewCryptoBalanceMovement(
		inv.ID,
		inv.GetDocumentType(),
		newVersion,
		inv.Date,
		entity.RecordTypeReceipt,
		*inv.WalletID,
		inv.TokenID,
		inv.ReceivedAmount,
	)

	return []entity.CryptoBalanceMovement{movement}, nil
}

func (inv *CryptoInvoice) GetLineCount() int { return len(inv.Lines) }

// Compile-time interface checks.
var _ posting.Postable = (*CryptoInvoice)(nil)
var _ posting.CryptoBalanceMovementSource = (*CryptoInvoice)(nil)
var _ posting.LineCounter = (*CryptoInvoice)(nil)
