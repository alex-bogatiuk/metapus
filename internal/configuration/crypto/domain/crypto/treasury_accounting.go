package crypto

import (
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

type TreasuryAccountingBasis struct {
	DocumentID, NetworkID, TokenID, SourceWalletID id.ID
	TxHash                                         string
	Amount                                         types.RawAmount
	VerificationID                                 *id.ID
}
