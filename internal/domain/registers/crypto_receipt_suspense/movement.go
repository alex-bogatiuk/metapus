package crypto_receipt_suspense

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// CryptoReceiptSuspenseMovement tracks economic attribution of one receipt.
// Its dimension is the receipt, not the current physical location of coins.
type CryptoReceiptSuspenseMovement struct {
	entity.MovementBase
	ReceiptID id.ID              `db:"receipt_id" json:"receiptId"`
	TokenID   id.ID              `db:"token_id" json:"tokenId"`
	Amount    types.CryptoAmount `db:"amount" json:"amount"`
}

func NewCryptoReceiptSuspenseMovement(
	recorderID id.ID, recorderType string, version int, period time.Time,
	recordType entity.RecordType, receiptID, tokenID id.ID, amount types.CryptoAmount,
) CryptoReceiptSuspenseMovement {
	return CryptoReceiptSuspenseMovement{
		MovementBase: entity.NewMovementBase(recorderID, recorderType, version, period, recordType),
		ReceiptID:    receiptID, TokenID: tokenID, Amount: amount,
	}
}
