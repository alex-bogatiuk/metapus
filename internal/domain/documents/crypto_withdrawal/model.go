// Package crypto_withdrawal provides the CryptoWithdrawal document.
// CryptoWithdrawal records a merchant's request to withdraw funds to an external address.
// Lifecycle: Created → Signed → Broadcast → Confirmed | Failed
package crypto_withdrawal

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/domain/posting"
)

// WithdrawalStatus defines the lifecycle state of a withdrawal.
type WithdrawalStatus string

const DocumentType = "CryptoWithdrawal"

const (
	WithdrawalStatusCreated   WithdrawalStatus = "created"   // merchant created the request
	WithdrawalStatusSigned    WithdrawalStatus = "signed"    // transaction signed by Signer
	WithdrawalStatusBroadcast WithdrawalStatus = "broadcast" // transaction broadcast to network
	WithdrawalStatusConfirmed WithdrawalStatus = "confirmed" // required confirmations reached
	WithdrawalStatusFailed    WithdrawalStatus = "failed"    // broadcast or confirmation failed
)

// CryptoWithdrawal represents a merchant withdrawal to an external address.
type CryptoWithdrawal struct {
	entity.Document
	AccountingModelVersion int `db:"accounting_model_version" json:"accountingModelVersion"`

	// MerchantID — who is withdrawing (DataScope dimension)
	MerchantID id.ID `db:"merchant_id" json:"merchantId" meta:"label:Мерчант,ref:merchant"`

	// TokenID identifies the token being withdrawn
	TokenID id.ID `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`

	// SourceWalletID — hot wallet used as source of funds
	SourceWalletID id.ID `db:"source_wallet_id" json:"sourceWalletId" meta:"label:Исходный кошелёк,ref:wallet"`

	// DestAddress — external address (merchant's own wallet)
	DestAddress string `db:"dest_address" json:"destAddress" meta:"label:Адрес назначения"`

	// Amount to withdraw (token minor units)
	Amount types.CryptoAmount `db:"amount" json:"amount" meta:"label:Сумма"`

	// NetworkFee paid for the transaction (filled after broadcast)
	NetworkFee        types.CryptoAmount `db:"network_fee" json:"networkFee" meta:"label:Комиссия сети"`
	NetworkFeeTokenID *id.ID             `db:"network_fee_token_id" json:"networkFeeTokenId,omitempty" meta:"label:Токен комиссии,ref:token"`

	// TxHash is the broadcast transaction hash (filled after broadcast)
	TxHash string `db:"tx_hash" json:"txHash,omitempty" meta:"label:TX Hash"`

	// Status is the current lifecycle state
	Status                   WithdrawalStatus                `db:"status" json:"status" meta:"label:Статус"`
	SettlementStatus         outbound_settlement.Status      `db:"settlement_status" json:"settlementStatus"`
	SettlementCheckedAt      *time.Time                      `db:"settlement_checked_at" json:"settlementCheckedAt,omitempty"`
	SettlementError          string                          `db:"settlement_error" json:"settlementError,omitempty"`
	SettlementClaimedBy      string                          `db:"settlement_claimed_by" json:"-"`
	SettlementClaimedAt      *time.Time                      `db:"settlement_claimed_at" json:"-"`
	SettlementLeaseExpiresAt *time.Time                      `db:"settlement_lease_expires_at" json:"-"`
	ChainResult              outbound_settlement.ChainResult `db:"chain_result" json:"chainResult,omitempty"`
	EnergyUsed               int64                           `db:"energy_used" json:"energyUsed"`
	NetFee                   types.CryptoAmount              `db:"net_fee" json:"netFee"`
	EnergyFee                types.CryptoAmount              `db:"energy_fee" json:"energyFee"`
	NetUsage                 int64                           `db:"net_usage" json:"netUsage"`
}

// NewCryptoWithdrawal creates a new CryptoWithdrawal in Created state.
func NewCryptoWithdrawal(
	merchantID, tokenID, sourceWalletID id.ID,
	destAddress string,
	amount types.CryptoAmount,
) *CryptoWithdrawal {
	return &CryptoWithdrawal{
		AccountingModelVersion: 2,
		Document:               entity.NewDocument(),
		MerchantID:             merchantID,
		TokenID:                tokenID,
		SourceWalletID:         sourceWalletID,
		DestAddress:            destAddress,
		Amount:                 amount,
		NetworkFee:             types.ZeroCryptoAmount(),
		Status:                 WithdrawalStatusCreated,
		SettlementStatus:       outbound_settlement.StatusNotApplicable,
	}
}

// Validate implements entity.Validatable.
func (w *CryptoWithdrawal) Validate(ctx context.Context) error {
	if err := w.Document.Validate(ctx); err != nil {
		return err
	}

	if id.IsNil(w.MerchantID) {
		return apperror.NewValidation("merchant is required").WithDetail("field", "merchantId")
	}
	if id.IsNil(w.TokenID) {
		return apperror.NewValidation("token is required").WithDetail("field", "tokenId")
	}
	if id.IsNil(w.SourceWalletID) {
		return apperror.NewValidation("source wallet is required").WithDetail("field", "sourceWalletId")
	}
	if w.DestAddress == "" {
		return apperror.NewValidation("destination address is required").WithDetail("field", "destAddress")
	}
	if !w.Amount.IsPositive() {
		return apperror.NewValidation("amount must be positive").WithDetail("field", "amount")
	}
	return nil
}

// --- RLSDimensionable override ---

// GetRLSDimensions returns merchant dimension for RLS filtering.
func (w *CryptoWithdrawal) GetRLSDimensions() map[string]string {
	return map[string]string{
		"merchant": w.MerchantID.String(),
	}
}

// --- Postable interface ---

func (w *CryptoWithdrawal) GetDocumentType() string { return DocumentType }

// GenerateCryptoBalanceMovements creates an EXPENSE movement from the source wallet.
func (w *CryptoWithdrawal) GenerateCryptoBalanceMovements(ctx context.Context) ([]entity.CryptoBalanceMovement, error) {
	if w.AccountingModelVersion == 2 {
		return nil, nil
	}
	if w.Amount.IsZero() {
		return nil, nil
	}

	newVersion := w.PostedVersion + 1

	movement := entity.NewCryptoBalanceMovement(
		w.ID,
		w.GetDocumentType(),
		newVersion,
		w.Date,
		entity.RecordTypeExpense,
		w.SourceWalletID,
		w.TokenID,
		w.Amount,
	)

	return []entity.CryptoBalanceMovement{movement}, nil
}

// GenerateCryptoFeeMovements creates a RECEIPT movement for withdrawal fee.
func (w *CryptoWithdrawal) GenerateCryptoFeeMovements(ctx context.Context) ([]entity.CryptoFeeMovement, error) {
	if w.AccountingModelVersion == 2 {
		return nil, nil
	}
	if w.NetworkFee.IsZero() {
		return nil, nil
	}

	newVersion := w.PostedVersion + 1

	fee := entity.NewCryptoFeeMovement(
		w.ID,
		w.GetDocumentType(),
		newVersion,
		w.Date,
		entity.RecordTypeReceipt,
		w.MerchantID,
		w.TokenID,
		entity.FeeTypeWithdrawal,
		w.NetworkFee,
	)

	return []entity.CryptoFeeMovement{fee}, nil
}

// GenerateCryptoMerchantBalanceMovements is intentionally a no-op.
// WithdrawalRequest uses the debit-first pattern and already records the merchant
// balance expense at request creation time. CryptoWithdrawal only moves crypto
// from a hot wallet and records optional fee income, so it must not debit the
// merchant balance a second time.
func (w *CryptoWithdrawal) GenerateCryptoMerchantBalanceMovements(ctx context.Context) ([]entity.CryptoMerchantBalanceMovement, error) {
	return nil, nil
}

// Chain-owned financial effects may only be reversed by a compensating event.
func (w *CryptoWithdrawal) CanUnpost(context.Context) error {
	return apperror.NewConflict("crypto withdrawal cannot be unposted; create a compensating adjustment")
}

// Compile-time interface checks.
var _ posting.Postable = (*CryptoWithdrawal)(nil)
var _ posting.UnpostGuard = (*CryptoWithdrawal)(nil)
var _ posting.CryptoBalanceMovementSource = (*CryptoWithdrawal)(nil)
var _ posting.CryptoFeeMovementSource = (*CryptoWithdrawal)(nil)
var _ posting.CryptoMerchantBalanceMovementSource = (*CryptoWithdrawal)(nil)
