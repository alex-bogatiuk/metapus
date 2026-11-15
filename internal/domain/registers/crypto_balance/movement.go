package crypto_balance

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// CryptoBalanceMovement represents a movement in the crypto balance register.
// Tracks cryptocurrency amounts for wallets by token.
// Uses CryptoAmount (big.Int) instead of MinorUnits — ETH has 18 decimals.
type CryptoBalanceMovement struct {
	entity.MovementBase

	// Dimensions
	WalletID id.ID `db:"wallet_id" json:"walletId"`
	TokenID  id.ID `db:"token_id" json:"tokenId"`

	// Resources — CryptoAmount for arbitrary precision
	Amount types.CryptoAmount `db:"amount" json:"amount"`
}

// NewCryptoBalanceMovement creates a new crypto balance movement.
func NewCryptoBalanceMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType entity.RecordType,
	walletID, tokenID id.ID,
	amount types.CryptoAmount,
) CryptoBalanceMovement {
	return CryptoBalanceMovement{
		MovementBase: entity.NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		WalletID:     walletID,
		TokenID:      tokenID,
		Amount:       amount,
	}
}
