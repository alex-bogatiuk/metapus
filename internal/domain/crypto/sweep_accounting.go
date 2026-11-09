package crypto

import (
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type SweepAccountingBasis struct {
	DocumentID, LineID, NetworkID, TokenID, SourceWalletID, DestinationWalletID id.ID
	TxHash                                                                      string
	Amount                                                                      types.CryptoAmount
	VerificationID                                                              *id.ID
}
