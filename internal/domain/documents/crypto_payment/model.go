// Package crypto_payment provides the CryptoPayment document.
// CryptoPayment records a confirmed blockchain transaction that pays a CryptoInvoice.
// Created automatically by the chain watcher — not manually editable.
package crypto_payment

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

// PaymentStatus defines the lifecycle state of a crypto payment (FSM).
type PaymentStatus string

const (
	PaymentStatusDetected   PaymentStatus = "detected"   // tx seen in mempool/block, 0 confirmations
	PaymentStatusConfirming PaymentStatus = "confirming"  // 1+ confirmations, not yet finalized
	PaymentStatusConfirmed  PaymentStatus = "confirmed"   // required confirmations reached
	PaymentStatusSettled    PaymentStatus = "settled"     // funds settled to merchant
	PaymentStatusReorged    PaymentStatus = "reorged"     // chain reorganization detected
)

// CryptoPayment represents a blockchain transaction that pays a CryptoInvoice.
type CryptoPayment struct {
	entity.Document

	// InvoiceID references the invoice being paid (FK → doc_crypto_invoices)
	InvoiceID id.ID `db:"invoice_id" json:"invoiceId" meta:"label:Инвойс,ref:crypto_invoice"`

	// MerchantID (denormalized for DataScope RLS filtering)
	MerchantID id.ID `db:"merchant_id" json:"merchantId" meta:"label:Мерчант,ref:merchant"`

	// TokenID identifies the token received
	TokenID id.ID `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`

	// WalletID is the receiving wallet address
	WalletID id.ID `db:"wallet_id" json:"walletId" meta:"label:Кошелёк,ref:wallet"`

	// TxHash is the blockchain transaction hash (unique per network)
	TxHash string `db:"tx_hash" json:"txHash" meta:"label:TX Hash"`

	// FromAddress is the sender's blockchain address
	FromAddress string `db:"from_address" json:"fromAddress" meta:"label:Отправитель"`

	// Amount received in this transaction (token minor units)
	Amount types.CryptoAmount `db:"amount" json:"amount" meta:"label:Сумма"`

	// BlockNumber where the transaction was included
	BlockNumber int64 `db:"block_number" json:"blockNumber" meta:"label:Блок"`

	// Confirmations is the current confirmation count
	Confirmations int `db:"confirmations" json:"confirmations" meta:"label:Подтверждения"`

	// RequiredConfs from BlockchainNetwork.ConfirmationsNeeded
	RequiredConfs int `db:"required_confs" json:"requiredConfs" meta:"label:Требуемые подтверждения"`

	// Status is the current FSM state
	Status PaymentStatus `db:"status" json:"status" meta:"label:Статус"`

	// NetworkFee is the blockchain fee for this transaction (informational)
	NetworkFee types.CryptoAmount `db:"network_fee" json:"networkFee" meta:"label:Комиссия сети"`

	// DetectedAt is when the transaction was first seen
	DetectedAt time.Time `db:"detected_at" json:"detectedAt" meta:"label:Обнаружен"`

	// ConfirmedAt is when the transaction reached required confirmations
	ConfirmedAt *time.Time `db:"confirmed_at" json:"confirmedAt,omitempty" meta:"label:Подтверждён"`
}

// NewCryptoPayment creates a new CryptoPayment in Detected state.
func NewCryptoPayment(
	invoiceID, merchantID, tokenID, walletID id.ID,
	txHash, fromAddress string,
	amount types.CryptoAmount,
	blockNumber int64,
	requiredConfs int,
) *CryptoPayment {
	doc := entity.NewDocument()
	doc.BasisType = "CryptoInvoice"
	doc.BasisID = &invoiceID

	return &CryptoPayment{
		Document:      doc,
		InvoiceID:     invoiceID,
		MerchantID:    merchantID,
		TokenID:       tokenID,
		WalletID:      walletID,
		TxHash:        txHash,
		FromAddress:   fromAddress,
		Amount:        amount,
		BlockNumber:   blockNumber,
		Confirmations: 0,
		RequiredConfs: requiredConfs,
		Status:        PaymentStatusDetected,
		NetworkFee:    types.ZeroCryptoAmount(),
		DetectedAt:    time.Now().UTC(),
	}
}

// Validate implements entity.Validatable — pure function, no DB calls.
func (p *CryptoPayment) Validate(ctx context.Context) error {
	if err := p.Document.Validate(ctx); err != nil {
		return err
	}

	if id.IsNil(p.InvoiceID) {
		return apperror.NewValidation("invoice is required").
			WithDetail("field", "invoiceId")
	}

	if id.IsNil(p.MerchantID) {
		return apperror.NewValidation("merchant is required").
			WithDetail("field", "merchantId")
	}

	if id.IsNil(p.TokenID) {
		return apperror.NewValidation("token is required").
			WithDetail("field", "tokenId")
	}

	if id.IsNil(p.WalletID) {
		return apperror.NewValidation("wallet is required").
			WithDetail("field", "walletId")
	}

	if p.TxHash == "" {
		return apperror.NewValidation("transaction hash is required").
			WithDetail("field", "txHash")
	}

	if !p.Amount.IsPositive() {
		return apperror.NewValidation("amount must be positive").
			WithDetail("field", "amount")
	}

	return nil
}

// --- CurrencyAwareDoc stubs (crypto uses TokenID, not CurrencyID) ---

func (p *CryptoPayment) GetCurrencyID() id.ID                    { return id.ID{} }
func (p *CryptoPayment) SetCurrencyID(_ id.ID)                    {}
func (p *CryptoPayment) ValidateCurrency(_ context.Context) error { return nil }
func (p *CryptoPayment) GetContractID() *id.ID                    { return nil }

// --- RLSDimensionable override ---

// GetRLSDimensions returns merchant dimension for RLS filtering.
func (p *CryptoPayment) GetRLSDimensions() map[string]string {
	return map[string]string{
		"merchant": p.MerchantID.String(),
	}
}

// --- Postable interface ---

func (p *CryptoPayment) GetDocumentType() string { return "CryptoPayment" }

// GenerateCryptoBalanceMovements creates a RECEIPT movement for the wallet.
func (p *CryptoPayment) GenerateCryptoBalanceMovements(ctx context.Context) ([]entity.CryptoBalanceMovement, error) {
	if p.Amount.IsZero() {
		return nil, nil
	}

	newVersion := p.PostedVersion + 1

	movement := entity.NewCryptoBalanceMovement(
		p.ID,
		p.GetDocumentType(),
		newVersion,
		p.Date,
		entity.RecordTypeReceipt,
		p.WalletID,
		p.TokenID,
		p.Amount,
	)

	return []entity.CryptoBalanceMovement{movement}, nil
}

// GenerateCryptoFeeMovements creates a RECEIPT movement for processing fees.
func (p *CryptoPayment) GenerateCryptoFeeMovements(ctx context.Context) ([]entity.CryptoFeeMovement, error) {
	// TODO: Calculate fee based on merchant.CommissionRate
	// For now, return empty — fees will be implemented in Phase 6 when we have
	// the merchant commission rate lookup integrated
	return nil, nil
}

// IsFullyConfirmed returns true if confirmations >= required.
func (p *CryptoPayment) IsFullyConfirmed() bool {
	return p.Confirmations >= p.RequiredConfs
}

// Compile-time interface checks.
var _ posting.Postable = (*CryptoPayment)(nil)
var _ posting.CryptoBalanceMovementSource = (*CryptoPayment)(nil)
var _ posting.CryptoFeeMovementSource = (*CryptoPayment)(nil)
