package crypto

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

type TreasuryAccountingBasis struct {
	DocumentID, NetworkID, TokenID, SourceWalletID id.ID
	TxHash                                         string
	Amount                                         types.RawAmount
	VerificationID                                 *id.ID
}
