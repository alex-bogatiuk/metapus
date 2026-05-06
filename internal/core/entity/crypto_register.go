package entity

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// ---------------------------------------------------------------------------
// Crypto Balance accumulation register
// ---------------------------------------------------------------------------

// CryptoBalanceMovement represents a movement in the crypto balance register.
// Tracks cryptocurrency amounts for wallets by token.
// Uses CryptoAmount (big.Int) instead of MinorUnits — ETH has 18 decimals.
type CryptoBalanceMovement struct {
	MovementBase

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
	recordType RecordType,
	walletID, tokenID id.ID,
	amount types.CryptoAmount,
) CryptoBalanceMovement {
	return CryptoBalanceMovement{
		MovementBase: NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		WalletID:     walletID,
		TokenID:      tokenID,
		Amount:       amount,
	}
}

// ---------------------------------------------------------------------------
// Crypto Fee accumulation register
// ---------------------------------------------------------------------------

// FeeType defines the type of crypto processing fee.
type FeeType string

const (
	FeeTypeProcessing FeeType = "processing" // platform processing fee
	FeeTypeNetwork    FeeType = "network"    // blockchain network fee (gas)
	FeeTypeWithdrawal FeeType = "withdrawal" // withdrawal fee
	FeeTypeSweep      FeeType = "sweep"      // sweep consolidation fee
)

// CryptoFeeMovement represents a movement in the crypto fee register.
// Tracks platform fees, network fees, and withdrawal fees.
type CryptoFeeMovement struct {
	MovementBase

	// Dimensions
	MerchantID id.ID   `db:"merchant_id" json:"merchantId"`
	TokenID    id.ID   `db:"token_id" json:"tokenId"`
	FeeType    FeeType `db:"fee_type" json:"feeType"`

	// Resources — CryptoAmount for arbitrary precision
	Amount types.CryptoAmount `db:"amount" json:"amount"`
}

// NewCryptoFeeMovement creates a new crypto fee movement.
func NewCryptoFeeMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType RecordType,
	merchantID, tokenID id.ID,
	feeType FeeType,
	amount types.CryptoAmount,
) CryptoFeeMovement {
	return CryptoFeeMovement{
		MovementBase: NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		MerchantID:   merchantID,
		TokenID:      tokenID,
		FeeType:      feeType,
		Amount:       amount,
	}
}
