package tron

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRiskWithdrawalSourcesAuthorizeEachLineExactlyOnce(t *testing.T) {
	first := MoneyMovementSourceLine{LineID: "line-a", WalletID: "wallet-a", SourceAddress: "source-a", SourceVaultKeyID: "key-a", AmountMinorUnits: RawAmount("60"), RiskRevision: 2, ScopeVersion: 1}
	second := MoneyMovementSourceLine{LineID: "line-b", WalletID: "wallet-b", SourceAddress: "source-b", SourceVaultKeyID: "key-b", AmountMinorUnits: RawAmount("40"), RiskRevision: 3, ScopeVersion: 1}
	intent := MoneyMovementIntent{SchemaVersion: "money_movement_intent.v2", Purpose: "withdrawal", BasisType: "WithdrawalRequest", BasisID: "parent", AmountMinorUnits: RawAmount("100"), RiskClass: "unknown", RiskAccepted: true, SourceLines: []MoneyMovementSourceLine{first, second}}
	auth := SigningAuthorization{Attempt: 1, Generation: 1, BasisType: "CryptoAMLWithdrawalLine", BasisID: first.LineID, SourceAddress: first.SourceAddress, VaultKeyID: first.SourceVaultKeyID, AmountMinorUnits: first.AmountMinorUnits}
	require.True(t, intent.authorizesSource(auth))
	auth.BasisID = second.LineID
	require.False(t, intent.authorizesSource(auth))
	auth.SourceAddress = second.SourceAddress
	auth.VaultKeyID = second.SourceVaultKeyID
	auth.AmountMinorUnits = second.AmountMinorUnits
	require.True(t, intent.authorizesSource(auth))
	intent.RiskAccepted = false
	require.False(t, intent.authorizesSource(auth))
	intent.RiskAccepted = true
	intent.SourceLines[1].AmountMinorUnits = RawAmount("41")
	require.False(t, intent.authorizesSource(auth))
	intent.SourceLines[1].AmountMinorUnits = RawAmount("40")
	intent.SourceLines[1].WalletID = first.WalletID
	require.False(t, intent.authorizesSource(auth))
}
