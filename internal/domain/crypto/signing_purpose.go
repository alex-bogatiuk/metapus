package crypto

type SigningPurpose string

const (
	SigningPurposeWithdrawal    SigningPurpose = "withdrawal"
	SigningPurposeSweep         SigningPurpose = "sweep"
	SigningPurposeTreasury      SigningPurpose = "treasury_withdrawal"
	SigningPurposeActivation    SigningPurpose = "activation"
	SigningPurposeNativeFunding SigningPurpose = "native_funding"
)

func (p SigningPurpose) Valid() bool {
	switch p {
	case SigningPurposeWithdrawal,
		SigningPurposeSweep,
		SigningPurposeTreasury,
		SigningPurposeActivation,
		SigningPurposeNativeFunding:
		return true
	default:
		return false
	}
}

func (p SigningPurpose) RequiresApprovalBundle() bool {
	return p == SigningPurposeWithdrawal || p == SigningPurposeTreasury
}
