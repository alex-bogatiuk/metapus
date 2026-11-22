package crypto

import (
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

type WithdrawalAccountingBasis struct {
	DocumentID, RequestID, LineID, MerchantID, NetworkID, TokenID, SourceWalletID id.ID
	TxHash                                                                        string
	Amount                                                                        types.RawAmount
	VerificationID                                                                *id.ID
}

func (b WithdrawalAccountingBasis) SigningBasis() (string, id.ID) {
	if !id.IsNil(b.LineID) {
		return "CryptoAMLWithdrawalLine", b.LineID
	}
	return "WithdrawalRequest", b.RequestID
}
