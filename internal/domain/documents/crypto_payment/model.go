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
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/domain/tariff"
)

// PaymentStatus defines the lifecycle state of a crypto payment (FSM).
type PaymentStatus string

const (
	PaymentStatusDetected   PaymentStatus = "detected"   // tx seen in mempool/block, 0 confirmations
	PaymentStatusConfirming PaymentStatus = "confirming" // 1+ confirmations, not yet finalized
	PaymentStatusConfirmed  PaymentStatus = "confirmed"  // required confirmations reached
	PaymentStatusSettled    PaymentStatus = "settled"    // funds settled to merchant
	PaymentStatusReorged    PaymentStatus = "reorged"    // chain reorganization detected
	PaymentStatusReplaced   PaymentStatus = "replaced"   // proven EVM nonce replacement
	PaymentStatusFailed     PaymentStatus = "failed"     // terminal chain execution failure
)

// CryptoPayment represents a blockchain transaction that pays a CryptoInvoice.
type CryptoPayment struct {
	RiskDisplay aml.AssessmentDisplay `db:"-" json:"-"`
	entity.Document

	// InvoiceID references the invoice being paid (FK → doc_crypto_invoices)
	InvoiceID id.ID `db:"invoice_id" json:"invoiceId" meta:"label:Инвойс,ref:crypto_invoice"`

	// MerchantID (denormalized for DataScope RLS filtering)
	MerchantID id.ID `db:"merchant_id" json:"merchantId" meta:"label:Мерчант,ref:merchant"`

	// TokenID identifies the token received
	TokenID id.ID `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`

	// WalletID is the receiving wallet address
	WalletID id.ID `db:"wallet_id" json:"walletId" meta:"label:Кошелёк,ref:wallet"`

	NetworkID id.ID `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`

	// TxHash is the blockchain transaction hash (unique per network)
	TxHash string `db:"tx_hash" json:"txHash" meta:"label:TX Hash"`

	EventIndex int64 `db:"event_index" json:"eventIndex" meta:"label:Индекс события"`

	EventKind      string `db:"event_kind" json:"eventKind" meta:"label:Тип события"`
	VerificationID *id.ID `db:"verification_id" json:"verificationId,omitempty" meta:"label:Доказательство перевода"`

	// ReceiptID is the custody receipt this payment attributes to the merchant.
	// The receipt owns the custody; the payment only consumes its suspense.
	ReceiptID *id.ID `db:"receipt_id" json:"receiptId,omitempty" meta:"label:Поступление,ref:crypto_custody_receipt"`

	// FromAddress is the sender's blockchain address
	FromAddress   string `db:"from_address" json:"fromAddress" meta:"label:Отправитель"`
	SourceAddress string `db:"source_address" json:"sourceAddress,omitempty" meta:"label:Отправитель транзакции"`

	// SourceNonce is populated by EVM adapters when the originating transaction
	// can be loaded. It is never inferred from a missing receipt.
	SourceNonce *int64 `db:"source_nonce" json:"sourceNonce,omitempty" meta:"label:Nonce отправителя"`

	// ReplacementTxHash points at the proven replacement transaction, if any.
	ReplacementTxHash string `db:"replacement_tx_hash" json:"replacementTxHash,omitempty" meta:"label:Замещающая транзакция"`

	// Amount received in this transaction (token minor units)
	Amount types.CryptoAmount `db:"amount" json:"amount" meta:"label:Сумма"`

	// BlockNumber where the transaction was included
	BlockNumber int64 `db:"block_number" json:"blockNumber" meta:"label:Блок"`

	BlockHash string `db:"block_hash" json:"blockHash" meta:"label:Хеш блока"`

	FinalityStatus string `db:"finality_status" json:"finalityStatus" meta:"label:Финальность"`

	Canonicality string `db:"canonicality" json:"canonicality" meta:"label:Каноничность"`

	// Confirmations is the current confirmation count
	Confirmations int `db:"confirmations" json:"confirmations" meta:"label:Подтверждения"`

	// RequiredConfs from BlockchainNetwork.ConfirmationsNeeded
	RequiredConfs int `db:"required_confs" json:"requiredConfs" meta:"label:Требуемые подтверждения"`

	// Status is the current FSM state
	Status PaymentStatus `db:"status" json:"status" meta:"label:Статус"`

	// Fee snapshot — snapshotted from reg_fee_schedule at payment creation time.
	// Formula: clamp(FeeFixed + Amount × FeePercentBP / 10000, FeeMin, FeeMax)
	FeeFixed     types.CryptoAmount `db:"fee_fixed" json:"feeFixed" meta:"label:Комиссия фикс"`
	FeePercentBP int                `db:"fee_percent_bp" json:"feePercentBp" meta:"label:Комиссия %"`
	FeeMin       types.CryptoAmount `db:"fee_min" json:"feeMin" meta:"label:Мин. комиссия"`
	FeeMax       types.CryptoAmount `db:"fee_max" json:"feeMax" meta:"label:Макс. комиссия"`

	// DetectedAt is when the transaction was first seen
	DetectedAt time.Time `db:"detected_at" json:"detectedAt" meta:"label:Обнаружен"`

	// ConfirmedAt is when the transaction reached required confirmations
	ConfirmedAt *time.Time `db:"confirmed_at" json:"confirmedAt,omitempty" meta:"label:Подтверждён"`

	ConfirmationClaimedBy      string     `db:"confirmation_claimed_by" json:"-" meta:"label:Confirmation worker"`
	ConfirmationClaimedAt      *time.Time `db:"confirmation_claimed_at" json:"-" meta:"label:Confirmation claim time"`
	ConfirmationLeaseExpiresAt *time.Time `db:"confirmation_lease_expires_at" json:"-" meta:"label:Confirmation lease expiry"`
	NextConfirmationCheckAt    time.Time  `db:"next_confirmation_check_at" json:"-" meta:"label:Next confirmation check"`
	ConfirmationCheckAttempts  int        `db:"confirmation_check_attempts" json:"-" meta:"label:Confirmation attempts"`
	LastConfirmationCheckedAt  *time.Time `db:"last_confirmation_checked_at" json:"-" meta:"label:Last confirmation check"`
}

// NewCryptoPayment creates a new CryptoPayment in Detected state.
func NewCryptoPayment(
	invoiceID, merchantID, tokenID, walletID, networkID id.ID,
	txHash string, eventIndex int64, eventKind, blockHash, fromAddress string,
	amount types.CryptoAmount,
	blockNumber int64,
	requiredConfs int,
) *CryptoPayment {
	doc := entity.NewDocument()
	doc.BasisType = "CryptoInvoice"
	doc.BasisID = &invoiceID
	detectedAt := time.Now().UTC()

	return &CryptoPayment{
		Document:                doc,
		InvoiceID:               invoiceID,
		MerchantID:              merchantID,
		TokenID:                 tokenID,
		WalletID:                walletID,
		NetworkID:               networkID,
		TxHash:                  txHash,
		EventIndex:              eventIndex,
		EventKind:               eventKind,
		BlockHash:               blockHash,
		FinalityStatus:          "provisional",
		Canonicality:            "canonical",
		FromAddress:             fromAddress,
		Amount:                  amount,
		BlockNumber:             blockNumber,
		Confirmations:           0,
		RequiredConfs:           requiredConfs,
		Status:                  PaymentStatusDetected,
		DetectedAt:              detectedAt,
		NextConfirmationCheckAt: detectedAt,
	}
}

// SetFeeConfig snapshots the resolved fee configuration onto this payment.
// Accepts raw values to avoid import cycle with crypto package.
// Should be called at creation time with the result of FeeConfigResolver.Resolve().
func (p *CryptoPayment) SetFeeConfig(fixedFee types.CryptoAmount, percentBP int, minFee, maxFee types.CryptoAmount) {
	p.FeeFixed = fixedFee
	p.FeePercentBP = percentBP
	p.FeeMin = minFee
	p.FeeMax = maxFee
}

// Tariff is the processing tariff snapshotted on this payment.
func (p *CryptoPayment) Tariff() tariff.Fee {
	return tariff.Fee{Fixed: p.FeeFixed, PercentBP: p.FeePercentBP, Min: p.FeeMin, Max: p.FeeMax}
}

// FeeAmount calculates the platform fee using the snapshotted fee config.
// Formula: clamp(FeeFixed + Amount × FeePercentBP / 10000, FeeMin, FeeMax)
func (p *CryptoPayment) FeeAmount() types.CryptoAmount {
	return p.Tariff().Amount(p.Amount)
}

// NetAmount returns amount minus commission fee.
func (p *CryptoPayment) NetAmount() types.CryptoAmount {
	_, net := p.Tariff().Split(p.Amount)
	return net
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
	if id.IsNil(p.NetworkID) {
		return apperror.NewValidation("network is required").WithDetail("field", "networkId")
	}
	if p.EventIndex < 0 {
		return apperror.NewValidation("event index must be non-negative").WithDetail("field", "eventIndex")
	}
	if p.EventKind != "native_value" && p.EventKind != "contract_log" && p.EventKind != "internal_value" {
		return apperror.NewValidation("event kind must be native_value or contract_log").WithDetail("field", "eventKind")
	}
	if p.Canonicality != "canonical" && p.Canonicality != "orphaned" && p.Canonicality != "unknown" {
		return apperror.NewValidation("canonicality must be canonical, orphaned, or unknown").WithDetail("field", "canonicality")
	}

	if p.TxHash == "" {
		return apperror.NewValidation("transaction hash is required").
			WithDetail("field", "txHash")
	}

	if !p.Amount.IsPositive() {
		return apperror.NewValidation("amount must be positive").
			WithDetail("field", "amount")
	}

	if p.FeePercentBP < 0 || p.FeePercentBP > 10000 {
		return apperror.NewValidation("fee percent must be between 0 and 10000 basis points").
			WithDetail("field", "feePercentBp").
			WithDetail("value", p.FeePercentBP)
	}

	return nil
}

// --- RLSDimensionable override ---

// GetRLSDimensions returns merchant dimension for RLS filtering.
func (p *CryptoPayment) GetRLSDimensions() map[string]string {
	return map[string]string{
		"merchant": p.MerchantID.String(),
	}
}

// --- Postable interface ---

func (p *CryptoPayment) GetDocumentType() string { return Definition.Name }

// GenerateCryptoReceiptSuspenseMovements consumes the gross of the attributed
// custody receipt; the custody movement itself belongs to the receipt.
func (p *CryptoPayment) GenerateCryptoReceiptSuspenseMovements(context.Context) ([]entity.CryptoReceiptSuspenseMovement, error) {
	if p.ReceiptID == nil || id.IsNil(*p.ReceiptID) {
		return nil, apperror.NewValidation("payment posting requires its custody receipt")
	}
	if p.FeeAmount().Cmp(p.Amount) > 0 {
		return nil, apperror.NewValidation("processing fee exceeds the received amount")
	}
	return []entity.CryptoReceiptSuspenseMovement{entity.NewCryptoReceiptSuspenseMovement(
		p.ID, p.GetDocumentType(), p.PostedVersion+1, p.Date, entity.RecordTypeExpense, *p.ReceiptID, p.TokenID, p.Amount)}, nil
}

// GenerateCryptoFeeMovements creates a RECEIPT movement for the processing fee.
// Fee = clamp(FeeFixed + Amount × FeePercentBP / 10000, FeeMin, FeeMax).
func (p *CryptoPayment) GenerateCryptoFeeMovements(ctx context.Context) ([]entity.CryptoFeeMovement, error) {
	feeAmount := p.FeeAmount()
	if feeAmount.IsZero() {
		return nil, nil
	}

	newVersion := p.PostedVersion + 1

	fee := entity.NewCryptoFeeMovement(
		p.ID,
		p.GetDocumentType(),
		newVersion,
		p.Date,
		entity.RecordTypeReceipt,
		p.MerchantID,
		p.TokenID,
		entity.FeeTypeProcessing,
		feeAmount,
	)

	return []entity.CryptoFeeMovement{fee}, nil
}

// GenerateCryptoMerchantBalanceMovements creates a RECEIPT movement for the merchant.
// Receipt = platform owes more to merchant (payment received, net of fees).
// Net amount = Amount - FeeAmount().
func (p *CryptoPayment) GenerateCryptoMerchantBalanceMovements(ctx context.Context) ([]entity.CryptoMerchantBalanceMovement, error) {
	netAmount := p.NetAmount()
	if netAmount.IsZero() {
		return nil, nil
	}
	newVersion := p.PostedVersion + 1

	movement := entity.NewCryptoMerchantBalanceMovement(
		p.ID,
		p.GetDocumentType(),
		newVersion,
		p.Date,
		entity.RecordTypeReceipt,
		p.MerchantID,
		p.TokenID,
		netAmount,
	)

	return []entity.CryptoMerchantBalanceMovement{movement}, nil
}

// IsFullyConfirmed returns true if confirmations >= required.
func (p *CryptoPayment) IsFullyConfirmed() bool {
	return p.Confirmations >= p.RequiredConfs
}

// Chain-owned financial effects may only be reversed by a compensating event.
func (p *CryptoPayment) CanUnpost(context.Context) error {
	return apperror.NewConflict("crypto payment cannot be unposted; create a compensating adjustment")
}

// Compile-time interface checks.
var _ posting.Postable = (*CryptoPayment)(nil)
var _ posting.UnpostGuard = (*CryptoPayment)(nil)
var _ cryptoposting.CryptoReceiptSuspenseMovementSource = (*CryptoPayment)(nil)
var _ cryptoposting.CryptoFeeMovementSource = (*CryptoPayment)(nil)
var _ cryptoposting.CryptoMerchantBalanceMovementSource = (*CryptoPayment)(nil)
