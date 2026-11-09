package crypto

import (
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type TreasuryAccountingBasis struct {
	DocumentID, NetworkID, TokenID, SourceWalletID id.ID
	TxHash                                         string
	Amount                                         types.CryptoAmount
	VerificationID                                 *id.ID
}
