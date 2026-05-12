// Package crypto_withdrawal provides the CryptoWithdrawal document.
// CryptoWithdrawal records a merchant's request to withdraw funds to an external address.
// Lifecycle: Created → Signed → Broadcast → Confirmed | Failed
package crypto_withdrawal

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

// WithdrawalStatus defines the lifecycle state of a withdrawal.
type WithdrawalStatus string

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
	NetworkFee types.CryptoAmount `db:"network_fee" json:"networkFee" meta:"label:Комиссия сети"`

	// TxHash is the broadcast transaction hash (filled after broadcast)
	TxHash string `db:"tx_hash" json:"txHash,omitempty" meta:"label:TX Hash"`

	// Status is the current lifecycle state
	Status WithdrawalStatus `db:"status" json:"status" meta:"label:Статус"`
}

// NewCryptoWithdrawal creates a new CryptoWithdrawal in Created state.
func NewCryptoWithdrawal(
	merchantID, tokenID, sourceWalletID id.ID,
	destAddress string,
	amount types.CryptoAmount,
) *CryptoWithdrawal {
	return &CryptoWithdrawal{
		Document:       entity.NewDocument(),
		MerchantID:     merchantID,
		TokenID:        tokenID,
		SourceWalletID: sourceWalletID,
		DestAddress:    destAddress,
		Amount:         amount,
		NetworkFee:     types.ZeroCryptoAmount(),
		Status:         WithdrawalStatusCreated,
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

// --- CurrencyAwareDoc stubs ---

func (w *CryptoWithdrawal) GetCurrencyID() id.ID                    { return id.ID{} }
func (w *CryptoWithdrawal) SetCurrencyID(_ id.ID)                    {}
func (w *CryptoWithdrawal) ValidateCurrency(_ context.Context) error { return nil }
func (w *CryptoWithdrawal) GetContractID() *id.ID                    { return nil }

// --- RLSDimensionable override ---

// GetRLSDimensions returns merchant dimension for RLS filtering.
func (w *CryptoWithdrawal) GetRLSDimensions() map[string]string {
	return map[string]string{
		"merchant": w.MerchantID.String(),
	}
}

// --- Postable interface ---

func (w *CryptoWithdrawal) GetDocumentType() string { return "CryptoWithdrawal" }

// GenerateCryptoBalanceMovements creates an EXPENSE movement from the source wallet.
func (w *CryptoWithdrawal) GenerateCryptoBalanceMovements(ctx context.Context) ([]entity.CryptoBalanceMovement, error) {
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

// Compile-time interface checks.
var _ posting.Postable = (*CryptoWithdrawal)(nil)
var _ posting.CryptoBalanceMovementSource = (*CryptoWithdrawal)(nil)
var _ posting.CryptoFeeMovementSource = (*CryptoWithdrawal)(nil)
