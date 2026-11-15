package crypto_merchant_balance

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// ---------------------------------------------------------------------------
// Crypto Merchant Balance accumulation register
// (Регистр «Крипто-расчёты с мерчантами»)
// Tracks platform debt to merchant: receipt = we owe more, expense = debt reduced.
// ---------------------------------------------------------------------------

// CryptoMerchantBalanceMovement represents a movement in the crypto merchant balance register.
// Receipt means the platform owes the merchant more (payment received).
// Expense means the platform paid the merchant (withdrawal processed).
type CryptoMerchantBalanceMovement struct {
	entity.MovementBase

	// Dimensions
	MerchantID id.ID `db:"merchant_id" json:"merchantId"`
	TokenID    id.ID `db:"token_id" json:"tokenId"`

	// Resources — CryptoAmount for arbitrary precision
	Amount types.CryptoAmount `db:"amount" json:"amount"`
}

// NewCryptoMerchantBalanceMovement creates a new crypto merchant balance movement.
func NewCryptoMerchantBalanceMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType entity.RecordType,
	merchantID, tokenID id.ID,
	amount types.CryptoAmount,
) CryptoMerchantBalanceMovement {
	return CryptoMerchantBalanceMovement{
		MovementBase: entity.NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		MerchantID:   merchantID,
		TokenID:      tokenID,
		Amount:       amount,
	}
}
