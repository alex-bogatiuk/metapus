package crypto

import (
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

const NativeFundingBasisType = "CryptoNativeFunding"

// NativeFunding is the durable gas transfer, not a lease of its parent command.
// Parent expiry, a kill switch or signer downtime cannot undo its chain effect.
type NativeFunding struct {
	ID, BasisID, NetworkID, NativeTokenID, SourceWalletID, DestinationWalletID id.ID
	BasisType                                                                  string
	Attempt                                                                    int
	Amount                                                                     types.RawAmount
	FeeReservationID, SigningAuthorizationID                                   *id.ID
	UnsignedFingerprint, TxHash, Status, LastError                             string
}

func (f NativeFunding) ValidateReceipt(receipt ChainReceipt) error {
	if id.IsNil(f.ID) || id.IsNil(f.NetworkID) || id.IsNil(f.NativeTokenID) || id.IsNil(f.SourceWalletID) || id.IsNil(f.DestinationWalletID) ||
		f.TxHash == "" || receipt.TxHash != f.TxHash || receipt.NetworkID != f.NetworkID ||
		receipt.Fee.NativeAsset.TokenID != f.NativeTokenID || !receipt.FinanciallyFinal() ||
		!f.Amount.IsPositive() || f.SourceWalletID == f.DestinationWalletID {
		return apperror.NewValidation("native funding requires a final receipt matching its immutable identity")
	}
	if f.Status != "broadcast" && f.Status != "solidified" && f.Status != "settled" && f.Status != "failed" {
		return apperror.NewConflict("native funding is not ready for settlement")
	}
	return nil
}
