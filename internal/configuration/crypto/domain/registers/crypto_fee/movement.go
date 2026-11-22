package crypto_fee

import (
	"time"

	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

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
	entity.MovementBase

	// Dimensions
	MerchantID id.ID   `db:"merchant_id" json:"merchantId"`
	TokenID    id.ID   `db:"token_id" json:"tokenId"`
	FeeType    FeeType `db:"fee_type" json:"feeType"`

	// Resources — RawAmount for arbitrary precision
	Amount types.RawAmount `db:"amount" json:"amount"`
}

// NewCryptoFeeMovement creates a new crypto fee movement.
func NewCryptoFeeMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType entity.RecordType,
	merchantID, tokenID id.ID,
	feeType FeeType,
	amount types.RawAmount,
) CryptoFeeMovement {
	return CryptoFeeMovement{
		MovementBase: entity.NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		MerchantID:   merchantID,
		TokenID:      tokenID,
		FeeType:      feeType,
		Amount:       amount,
	}
}
