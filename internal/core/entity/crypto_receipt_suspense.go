package entity

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// CryptoReceiptSuspenseMovement tracks economic attribution of one receipt.
// Its dimension is the receipt, not the current physical location of coins.
type CryptoReceiptSuspenseMovement struct {
	MovementBase
	ReceiptID id.ID              `db:"receipt_id" json:"receiptId"`
	TokenID   id.ID              `db:"token_id" json:"tokenId"`
	Amount    types.CryptoAmount `db:"amount" json:"amount"`
}

func NewCryptoReceiptSuspenseMovement(
	recorderID id.ID, recorderType string, version int, period time.Time,
	recordType RecordType, receiptID, tokenID id.ID, amount types.CryptoAmount,
) CryptoReceiptSuspenseMovement {
	return CryptoReceiptSuspenseMovement{
		MovementBase: NewMovementBase(recorderID, recorderType, version, period, recordType),
		ReceiptID:    receiptID, TokenID: tokenID, Amount: amount,
	}
}
