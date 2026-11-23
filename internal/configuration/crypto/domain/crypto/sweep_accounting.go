package crypto

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

type SweepAccountingBasis struct {
	DocumentID, LineID, NetworkID, TokenID, SourceWalletID, DestinationWalletID id.ID
	TxHash                                                                      string
	Amount                                                                      types.RawAmount
	VerificationID                                                              *id.ID
}
