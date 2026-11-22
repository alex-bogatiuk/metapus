package crypto

import (
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

type SweepAccountingBasis struct {
	DocumentID, LineID, NetworkID, TokenID, SourceWalletID, DestinationWalletID id.ID
	TxHash                                                                      string
	Amount                                                                      types.RawAmount
	VerificationID                                                              *id.ID
}
