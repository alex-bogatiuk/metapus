package crypto

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

func TestApprovalPolicyRequiredPrincipals(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	policy := DefaultMoneyMovementApprovalPolicy("tenant-1", now)

	tests := []struct {
		name       string
		intent     MoneyMovementIntent
		risk       RiskAssessment
		want       []ApprovalPrincipal
		wantRules  []string
		wantErrMsg string
	}{
		{
			name:      "small low risk withdrawal requires only risk",
			intent:    testMoneyMovementIntent(SigningPurposeWithdrawal, 100_000_000),
			risk:      RiskAssessment{RiskLevel: ApprovalRiskLevelLow},
			want:      []ApprovalPrincipal{ApprovalPrincipalRisk},
			wantRules: []string{"small-low-risk-withdrawal"},
		},
		{
			name:      "small treasury requires only admin",
			intent:    testMoneyMovementIntent(SigningPurposeTreasury, 500_000_000),
			risk:      RiskAssessment{RiskLevel: ApprovalRiskLevelLow},
			want:      []ApprovalPrincipal{ApprovalPrincipalAdmin},
			wantRules: []string{"manual-treasury-small"},
		},
		{
			name:      "high value withdrawal requires admin and risk",
			intent:    testMoneyMovementIntent(SigningPurposeWithdrawal, 5_000_000_000),
			risk:      RiskAssessment{RiskLevel: ApprovalRiskLevelHigh},
			want:      []ApprovalPrincipal{ApprovalPrincipalAdmin, ApprovalPrincipalRisk},
			wantRules: []string{"high-value"},
		},
		{
			name:      "unmatched intent falls back to default admin and risk",
			intent:    testMoneyMovementIntent(SigningPurposeWithdrawal, 1_000_000_000),
			risk:      RiskAssessment{RiskLevel: ApprovalRiskLevelMedium},
			want:      []ApprovalPrincipal{ApprovalPrincipalAdmin, ApprovalPrincipalRisk},
			wantRules: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rules, err := policy.RequiredPrincipals(tt.intent, tt.risk)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantRules, rules)
		})
	}
}

func TestApprovalPolicyOverlappingRulesUnionPrincipals(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	limit := types.NewRawAmountFromInt64(1_000_000)
	policy := ApprovalPolicyBundle{
		SchemaVersion:             ApprovalPolicySchemaV1,
		PolicyVersion:             "test-policy",
		TenantID:                  "tenant-1",
		DefaultRequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalRisk},
		Rules: []ApprovalPolicyRule{
			{ID: "admin-by-purpose", Purpose: SigningPurposeWithdrawal, RequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalAdmin}},
			{ID: "risk-by-amount", AmountMinorLTE: &limit, RequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalRisk}},
		},
		EffectiveFrom: now.Add(-time.Hour),
		ExpiresAt:     now.Add(time.Hour),
	}
	require.NoError(t, policy.Validate(now))

	required, rules, err := policy.RequiredPrincipals(testMoneyMovementIntent(SigningPurposeWithdrawal, 1_000_000), RiskAssessment{RiskLevel: ApprovalRiskLevelLow})
	require.NoError(t, err)
	require.Equal(t, []ApprovalPrincipal{ApprovalPrincipalAdmin, ApprovalPrincipalRisk}, required)
	require.Equal(t, []string{"admin-by-purpose", "risk-by-amount"}, rules)
}

func TestApprovalPolicyRejectsEmptyRequirements(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	policy := ApprovalPolicyBundle{
		SchemaVersion:             ApprovalPolicySchemaV1,
		PolicyVersion:             "bad-policy",
		TenantID:                  "tenant-1",
		DefaultRequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalRisk},
		Rules: []ApprovalPolicyRule{
			{ID: "empty", Purpose: SigningPurposeWithdrawal},
		},
		EffectiveFrom: now.Add(-time.Hour),
		ExpiresAt:     now.Add(time.Hour),
	}

	require.ErrorContains(t, policy.Validate(now), "required principals cannot be empty")
}

func TestMoneyMovementApprovalSignerBuildsAndVerifiesRequiredArtifacts(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	keySet := newTestApprovalKeySet(t)
	signer := testApprovalSigner(t, keySet, now)
	keys := keySet.publicKeys()
	adminID := id.New()

	tests := []struct {
		name      string
		intent    MoneyMovementIntent
		adminID   *id.ID
		stepUp    string
		want      []ApprovalPrincipal
		verifyErr string
		buildErr  string
	}{
		{
			name:    "risk only succeeds without admin approval",
			intent:  testMoneyMovementIntent(SigningPurposeWithdrawal, 100_000_000),
			want:    []ApprovalPrincipal{ApprovalPrincipalRisk},
			stepUp:  "",
			adminID: nil,
		},
		{
			name:    "admin only succeeds with object bound step up digest",
			intent:  testMoneyMovementIntent(SigningPurposeTreasury, 500_000_000),
			want:    []ApprovalPrincipal{ApprovalPrincipalAdmin},
			adminID: &adminID,
			stepUp:  "step-up-digest",
		},
		{
			name:     "admin only rejects missing step up digest",
			intent:   testMoneyMovementIntent(SigningPurposeTreasury, 500_000_000),
			adminID:  &adminID,
			buildErr: "admin approval requires step-up proof digest",
		},
		{
			name:      "both required rejects without admin approval",
			intent:    testMoneyMovementIntent(SigningPurposeWithdrawal, 5_000_000_000),
			want:      []ApprovalPrincipal{ApprovalPrincipalAdmin, ApprovalPrincipalRisk},
			verifyErr: "missing required admin approval",
		},
		{
			name:    "both required succeeds with admin and risk approvals",
			intent:  testMoneyMovementIntent(SigningPurposeWithdrawal, 5_000_000_000),
			want:    []ApprovalPrincipal{ApprovalPrincipalAdmin, ApprovalPrincipalRisk},
			adminID: &adminID,
			stepUp:  "step-up-digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifacts, err := signer.BuildArtifacts(context.Background(), tt.intent, tt.adminID, tt.stepUp)
			if tt.buildErr != "" {
				require.ErrorContains(t, err, tt.buildErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, artifacts.RequiredPrincipals)

			verified, err := VerifyApprovalBundle(artifacts.Bundle, keys, now)
			if tt.verifyErr != "" {
				require.ErrorContains(t, err, tt.verifyErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, verified.Decision.RequiredPrincipals)
			require.Equal(t, artifacts.IntentDigest, verified.IntentDigest)
		})
	}
}

func TestVerifyApprovalBundleClassifiesExpiredArtifacts(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	keySet := newTestApprovalKeySet(t)
	signer := testApprovalSigner(t, keySet, now)
	artifacts, err := signer.BuildArtifacts(context.Background(), testMoneyMovementIntent(SigningPurposeWithdrawal, 100_000_000), nil, "")
	require.NoError(t, err)

	_, err = VerifyApprovalBundle(artifacts.Bundle, keySet.publicKeys(), now.Add(DefaultApprovalTTL+time.Second))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrApprovalArtifactExpired), "expiration must remain machine-classifiable through wrapping: %v", err)
}

func TestVerifyApprovalBundleRejectsRiskApprovalForDifferentAssessment(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	keySet := newTestApprovalKeySet(t)
	signer := testApprovalSigner(t, keySet, now)
	keys := keySet.publicKeys()

	artifacts, err := signer.BuildArtifacts(context.Background(), testMoneyMovementIntent(SigningPurposeWithdrawal, 100_000_000), nil, "")
	require.NoError(t, err)
	require.Len(t, artifacts.Bundle.Approvals, 1)

	var approval MoneyMovementApproval
	_, err = VerifyEdDSAJWS(artifacts.Bundle.Approvals[0].JWS, keySet.riskApprovalPub, &approval)
	require.NoError(t, err)
	approval.RiskAssessmentDigest = "different-risk-assessment"
	artifacts.Bundle.Approvals[0].JWS, err = SignEdDSAJWS(approval, keySet.riskApprovalPriv)
	require.NoError(t, err)

	_, err = VerifyApprovalBundle(artifacts.Bundle, keys, now)
	require.ErrorContains(t, err, "risk_assessment_digest does not match")
}

func TestVerifyApprovalBundleRejectsApprovalSignedByWrongPrincipalKey(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	keySet := newTestApprovalKeySet(t)
	signer := testApprovalSigner(t, keySet, now)

	artifacts, err := signer.BuildArtifacts(context.Background(), testMoneyMovementIntent(SigningPurposeWithdrawal, 100_000_000), nil, "")
	require.NoError(t, err)
	require.Len(t, artifacts.Bundle.Approvals, 1)

	var approval MoneyMovementApproval
	_, err = VerifyEdDSAJWS(artifacts.Bundle.Approvals[0].JWS, keySet.riskApprovalPub, &approval)
	require.NoError(t, err)
	artifacts.Bundle.Approvals[0].JWS, err = SignEdDSAJWS(approval, keySet.adminApprovalPriv)
	require.NoError(t, err)

	_, err = VerifyApprovalBundle(artifacts.Bundle, keySet.publicKeys(), now)
	require.ErrorContains(t, err, "verify risk approval")
}

func TestBuildAdminApprovalForExistingBundleCompletesFrozenDecision(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	keySet := newTestApprovalKeySet(t)
	signer := testApprovalSigner(t, keySet, now)
	keys := keySet.publicKeys()

	intent := testMoneyMovementIntent(SigningPurposeWithdrawal, 5_000_000_000)
	artifacts, err := signer.BuildArtifacts(context.Background(), intent, nil, "")
	require.NoError(t, err)
	require.Equal(t, []ApprovalPrincipal{ApprovalPrincipalAdmin, ApprovalPrincipalRisk}, artifacts.RequiredPrincipals)

	_, err = VerifyApprovalBundle(artifacts.Bundle, keys, now)
	require.ErrorContains(t, err, "missing required admin approval")

	adminID := id.New()
	adminApproval, err := signer.BuildAdminApprovalForBundle(context.Background(), artifacts.Bundle, adminID, "object-bound-step-up")
	require.NoError(t, err)

	completed := artifacts.Bundle
	completed.Approvals = append(completed.Approvals, adminApproval.Bundle)
	verified, err := VerifyApprovalBundle(completed, keys, now)
	require.NoError(t, err)
	require.Equal(t, artifacts.IntentDigest, verified.IntentDigest)
	require.Equal(t, artifacts.PolicyDecisionDigest, verified.PolicyDecisionDigest)
	require.Len(t, verified.Approvals, 2)
}

type testApprovalKeySet struct {
	policyPub         ed25519.PublicKey
	policyPriv        ed25519.PrivateKey
	intentPub         ed25519.PublicKey
	intentPriv        ed25519.PrivateKey
	riskPub           ed25519.PublicKey
	riskPriv          ed25519.PrivateKey
	decisionPub       ed25519.PublicKey
	decisionPriv      ed25519.PrivateKey
	adminApprovalPub  ed25519.PublicKey
	adminApprovalPriv ed25519.PrivateKey
	riskApprovalPub   ed25519.PublicKey
	riskApprovalPriv  ed25519.PrivateKey
}

func newTestApprovalKeySet(t *testing.T) testApprovalKeySet {
	t.Helper()
	policyPub, policyPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	intentPub, intentPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	riskPub, riskPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	decisionPub, decisionPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	adminPub, adminPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	riskApprovalPub, riskApprovalPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return testApprovalKeySet{
		policyPub:         policyPub,
		policyPriv:        policyPriv,
		intentPub:         intentPub,
		intentPriv:        intentPriv,
		riskPub:           riskPub,
		riskPriv:          riskPriv,
		decisionPub:       decisionPub,
		decisionPriv:      decisionPriv,
		adminApprovalPub:  adminPub,
		adminApprovalPriv: adminPriv,
		riskApprovalPub:   riskApprovalPub,
		riskApprovalPriv:  riskApprovalPriv,
	}
}

func (k testApprovalKeySet) SignApprovalJWS(_ context.Context, _ string, role ApprovalSigningRole, payload any) (string, error) {
	privateKeys := map[ApprovalSigningRole]ed25519.PrivateKey{
		ApprovalSigningRoleIntent:         k.intentPriv,
		ApprovalSigningRoleRiskAssessment: k.riskPriv,
		ApprovalSigningRolePolicyDecision: k.decisionPriv,
		ApprovalSigningRoleAdminApproval:  k.adminApprovalPriv,
		ApprovalSigningRoleRiskApproval:   k.riskApprovalPriv,
	}
	return SignEdDSAJWS(payload, privateKeys[role])
}

func (k testApprovalKeySet) publicKeys() ApprovalPublicKeys {
	return ApprovalPublicKeys{
		PolicyPublicKey:        k.policyPub,
		IntentPublicKey:        k.intentPub,
		RiskPublicKey:          k.riskPub,
		DecisionPublicKey:      k.decisionPub,
		AdminApprovalPublicKey: k.adminApprovalPub,
		RiskApprovalPublicKey:  k.riskApprovalPub,
	}
}

func testApprovalSigner(t *testing.T, keys testApprovalKeySet, now time.Time) *MoneyMovementApprovalSigner {
	t.Helper()
	policyJWS, err := SignEdDSAJWS(DefaultMoneyMovementApprovalPolicy("tenant-1", now), keys.policyPriv)
	require.NoError(t, err)
	signedPolicy, err := NewSignedApprovalPolicy(policyJWS, keys.policyPub, now)
	require.NoError(t, err)
	signer, err := NewMoneyMovementApprovalSigner(keys, keys.publicKeys(), signedPolicy)
	require.NoError(t, err)
	signer.now = func() time.Time { return now }
	return signer
}

func testMoneyMovementIntent(purpose SigningPurpose, amount int64) MoneyMovementIntent {
	merchantID := id.New()
	intent := MoneyMovementIntent{
		SchemaVersion:      MoneyMovementIntentV1,
		TenantID:           "tenant-1",
		Purpose:            purpose,
		BasisType:          "test_basis",
		BasisID:            id.New(),
		MerchantID:         &merchantID,
		NetworkCode:        "tron_mainnet",
		TokenID:            id.New(),
		TokenStandard:      "native",
		AmountMinorUnits:   types.NewRawAmountFromInt64(amount),
		DestinationAddress: "TQZiVXDjShcTrAKbkYuoH6dVvnpcZANw7V",
		AddressID:          id.New(),
	}
	if purpose == SigningPurposeTreasury {
		intent.SourceAddress = "TLa2f6VPqDgRE67v1736s7bJ8Ray5wYjU7"
		intent.SourceVaultKeyID = "tenant-1/tron_mainnet/m/44'/195'/0'/0/0"
	}
	return intent
}
