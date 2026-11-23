package crypto_balance

import (
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// CryptoBalanceMovement represents a movement in the crypto balance register.
// Tracks cryptocurrency amounts for wallets by token.
// Uses RawAmount (big.Int) instead of MinorUnits — ETH has 18 decimals.
type CryptoBalanceMovement struct {
	entity.MovementBase

	// Dimensions
	WalletID id.ID `db:"wallet_id" json:"walletId"`
	TokenID  id.ID `db:"token_id" json:"tokenId"`

	// Resources — RawAmount for arbitrary precision
	Amount types.RawAmount `db:"amount" json:"amount"`
}

// NewCryptoBalanceMovement creates a new crypto balance movement.
func NewCryptoBalanceMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType entity.RecordType,
	walletID, tokenID id.ID,
	amount types.RawAmount,
) CryptoBalanceMovement {
	return CryptoBalanceMovement{
		MovementBase: entity.NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		WalletID:     walletID,
		TokenID:      tokenID,
		Amount:       amount,
	}
}
