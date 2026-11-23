package crypto_merchant_balance

import (
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
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

	// Resources — RawAmount for arbitrary precision
	Amount types.RawAmount `db:"amount" json:"amount"`
}

// NewCryptoMerchantBalanceMovement creates a new crypto merchant balance movement.
func NewCryptoMerchantBalanceMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType entity.RecordType,
	merchantID, tokenID id.ID,
	amount types.RawAmount,
) CryptoMerchantBalanceMovement {
	return CryptoMerchantBalanceMovement{
		MovementBase: entity.NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		MerchantID:   merchantID,
		TokenID:      tokenID,
		Amount:       amount,
	}
}
