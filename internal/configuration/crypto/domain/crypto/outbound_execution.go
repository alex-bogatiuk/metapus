package crypto

import (
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type SpendWallet struct {
	ID         id.ID
	Address    string
	VaultKeyID string
	Balance    types.RawAmount
}

type WithdrawalExecutionTask struct {
	AMLParentID     id.ID
	AMLSource       SpendWallet
	AMLLeaseToken   id.ID
	RecoveryOnly    bool
	RequestID       id.ID
	MerchantID      id.ID
	TokenID         id.ID
	NetworkID       id.ID
	NetworkCode     string
	Amount          types.RawAmount
	DestAddress     string
	ContractAddress string
	TokenStandard   token.TokenStandard
	TokenDecimals   int
	RequiredConfs   int
	PostedVersion   int
}

func (t WithdrawalExecutionTask) SigningBasis() string {
	if !id.IsNil(t.AMLParentID) {
		return "CryptoAMLWithdrawalLine"
	}
	return "WithdrawalRequest"
}
func (t WithdrawalExecutionTask) ApprovalRequestID() id.ID {
	if !id.IsNil(t.AMLParentID) {
		return t.AMLParentID
	}
	return t.RequestID
}
