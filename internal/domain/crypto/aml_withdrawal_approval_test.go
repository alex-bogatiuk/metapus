package crypto

import (
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"

	"github.com/stretchr/testify/require"
)

func riskApprovalIntent() MoneyMovementIntent {
	intent := testMoneyMovementIntent(SigningPurposeWithdrawal, 100)
	intent.BasisType = "WithdrawalRequest"
	intent.SchemaVersion = MoneyMovementIntentV2
	intent.RiskClass = "high"
	intent.RiskAccepted = true
	intent.SourceLines = []MoneyMovementSourceLine{{LineID: id.New(), WalletID: id.New(), SourceAddress: "source-a", SourceVaultKeyID: "key-a", AmountMinorUnits: types.MustCryptoAmount("60"), RiskRevision: 2, ScopeVersion: 1, EvidenceHash: "evidence-a"}, {LineID: id.New(), WalletID: id.New(), SourceAddress: "source-b", SourceVaultKeyID: "key-b", AmountMinorUnits: types.MustCryptoAmount("40"), RiskRevision: 3, ScopeVersion: 1, EvidenceHash: "evidence-b"}}
	return intent
}
func TestAMLWithdrawalApprovalV2BindsExactSourceLinesAndAcceptance(t *testing.T) {
	intent := riskApprovalIntent()
	require.NoError(t, intent.ValidateSources())
	for _, line := range intent.SourceLines {
		require.True(t, intent.AuthorizesSource("CryptoAMLWithdrawalLine", line.LineID, line.SourceAddress, line.SourceVaultKeyID, line.AmountMinorUnits))
	}
	first := intent.SourceLines[0]
	require.False(t, intent.AuthorizesSource("CryptoAMLWithdrawalLine", first.LineID, "source-b", first.SourceVaultKeyID, first.AmountMinorUnits))
	require.False(t, intent.AuthorizesSource("CryptoAMLWithdrawalLine", first.LineID, first.SourceAddress, first.SourceVaultKeyID, intent.AmountMinorUnits))
	require.False(t, intent.AuthorizesSource("WithdrawalRequest", intent.BasisID, first.SourceAddress, first.SourceVaultKeyID, intent.AmountMinorUnits))
	intent.RiskAccepted = false
	require.Error(t, intent.ValidateSources())
	intent.RiskAccepted = true
	intent.SourceLines[1].WalletID = first.WalletID
	require.Error(t, intent.ValidateSources())
}
func TestAMLWithdrawalApprovalRiskCannotBecomeStaticLow(t *testing.T) {
	now := time.Now().UTC()
	keys := newTestApprovalKeySet(t)
	signer := testApprovalSigner(t, keys, now)
	intent := riskApprovalIntent()
	admin := id.New()
	artifacts, err := signer.BuildArtifacts(t.Context(), intent, &admin, "step-up")
	require.NoError(t, err)
	require.Equal(t, MoneyMovementIntentV2, artifacts.Intent.SchemaVersion)
	require.Equal(t, ApprovalRiskLevelHigh, artifacts.RiskAssessment.RiskLevel)
	_, err = VerifyApprovalBundle(artifacts.Bundle, keys.publicKeys(), now)
	require.NoError(t, err)
}

func TestApprovalSigningRequiresExplicitIntentSchema(t *testing.T) {
	now := time.Now().UTC()
	signer := testApprovalSigner(t, newTestApprovalKeySet(t), now)
	for _, schema := range []string{"", "money_movement_intent.v99"} {
		intent := testMoneyMovementIntent(SigningPurposeWithdrawal, 100)
		intent.SchemaVersion = schema
		_, err := signer.BuildArtifacts(t.Context(), intent, nil, "")
		require.Error(t, err, "an unspecified or unsupported contract must not be upgraded implicitly")
	}
}
func TestLegacyIntentEncodingOmitsAllNewRiskFields(t *testing.T) {
	raw, err := json.MarshalDeterministic(testMoneyMovementIntent(SigningPurposeWithdrawal, 100))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "risk_class")
	require.NotContains(t, string(raw), "risk_accepted")
	require.NotContains(t, string(raw), "source_lines")
}
