package crypto

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"slices"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type MoneyMovementApprovalArtifacts struct {
	Bundle               ApprovalBundle
	Intent               MoneyMovementIntent
	IntentDigest         string
	RiskAssessment       RiskAssessment
	RiskDigest           string
	PolicyDecision       ApprovalPolicyDecision
	PolicyDecisionDigest string
	Approvals            []MoneyMovementApproval
	RequiredPrincipals   []ApprovalPrincipal
	MatchedRuleIDs       []string
}

type MoneyMovementSignedApproval struct {
	Approval MoneyMovementApproval
	Bundle   ApprovalBundleApproval
}

type ApprovalSigningRole string

const (
	ApprovalSigningRoleIntent         ApprovalSigningRole = "intent"
	ApprovalSigningRoleRiskAssessment ApprovalSigningRole = "risk-assessment"
	ApprovalSigningRolePolicyDecision ApprovalSigningRole = "policy-decision"
	ApprovalSigningRoleAdminApproval  ApprovalSigningRole = "admin-approval"
	ApprovalSigningRoleRiskApproval   ApprovalSigningRole = "risk-approval"
)

// ApprovalJWSSigner is the outbound port for approval artifact signatures.
// Implementations must keep private key material outside the application
// process (for example, in Vault Transit).
type ApprovalJWSSigner interface {
	SignApprovalJWS(ctx context.Context, tenantID string, role ApprovalSigningRole, payload any) (string, error)
}

type SignedApprovalPolicy struct {
	JWS    string
	Policy ApprovalPolicyBundle
	Digest string
}

type MoneyMovementApprovalSigner struct {
	jwsSigner    ApprovalJWSSigner
	publicKeys   ApprovalPublicKeys
	signedPolicy SignedApprovalPolicy
	now          func() time.Time
}

type MoneyMovementApprovalSignerProvider interface {
	SignerForTenant(tenantID string) (*MoneyMovementApprovalSigner, error)
	PublicKeysForTenant(tenantID string) (ApprovalPublicKeys, error)
}

type StaticMoneyMovementApprovalVerifierProvider struct {
	publicKeys ApprovalPublicKeys
}

func NewSignedApprovalPolicy(policyJWS string, policyPublicKey ed25519.PublicKey, now time.Time) (SignedApprovalPolicy, error) {
	var signed SignedApprovalPolicy
	if policyJWS == "" {
		return signed, fmt.Errorf("approval policy JWS is required")
	}
	if len(policyPublicKey) != ed25519.PublicKeySize {
		return signed, fmt.Errorf("%w: approval policy public key must be %d bytes", ErrInvalidSigningKey, ed25519.PublicKeySize)
	}
	var policy ApprovalPolicyBundle
	payload, err := VerifyEdDSAJWS(policyJWS, policyPublicKey, &policy)
	if err != nil {
		return signed, fmt.Errorf("verify signed approval policy: %w", err)
	}
	if err := policy.Validate(now); err != nil {
		return signed, err
	}
	return SignedApprovalPolicy{
		JWS:    policyJWS,
		Policy: policy,
		Digest: digestBytes(payload),
	}, nil
}

func NewStaticMoneyMovementApprovalVerifierProvider(publicKeys ApprovalPublicKeys) (*StaticMoneyMovementApprovalVerifierProvider, error) {
	if err := ValidateApprovalPublicKeys(publicKeys); err != nil {
		return nil, err
	}
	return &StaticMoneyMovementApprovalVerifierProvider{publicKeys: publicKeys}, nil
}

func (p *StaticMoneyMovementApprovalVerifierProvider) SignerForTenant(string) (*MoneyMovementApprovalSigner, error) {
	return nil, fmt.Errorf("money movement approval verifier cannot sign")
}

func (p *StaticMoneyMovementApprovalVerifierProvider) PublicKeysForTenant(string) (ApprovalPublicKeys, error) {
	return p.publicKeys, nil
}

func NewMoneyMovementApprovalSigner(jwsSigner ApprovalJWSSigner, publicKeys ApprovalPublicKeys, signedPolicy SignedApprovalPolicy) (*MoneyMovementApprovalSigner, error) {
	if jwsSigner == nil {
		return nil, fmt.Errorf("approval JWS signer is required")
	}
	if err := ValidateApprovalPublicKeys(publicKeys); err != nil {
		return nil, err
	}
	if signedPolicy.JWS == "" || signedPolicy.Digest == "" {
		return nil, fmt.Errorf("signed approval policy is incomplete")
	}
	s := &MoneyMovementApprovalSigner{
		jwsSigner:    jwsSigner,
		publicKeys:   publicKeys,
		signedPolicy: signedPolicy,
		now:          func() time.Time { return time.Now().UTC() },
	}
	if err := signedPolicy.Policy.Validate(s.now()); err != nil {
		return nil, err
	}
	return s, nil
}

func DefaultMoneyMovementApprovalPolicy(tenantID string, now time.Time) ApprovalPolicyBundle {
	lowWithdrawal := types.NewRawAmountFromInt64(100_000_000)
	smallTreasury := types.NewRawAmountFromInt64(500_000_000)
	highValue := types.NewRawAmountFromInt64(5_000_000_000)
	effectiveFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	if now.Before(effectiveFrom) {
		effectiveFrom = now.Add(-time.Hour)
	}
	return ApprovalPolicyBundle{
		SchemaVersion:             ApprovalPolicySchemaV1,
		PolicyVersion:             DefaultApprovalPolicyV1,
		TenantID:                  tenantID,
		DefaultRequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalAdmin, ApprovalPrincipalRisk},
		Rules: []ApprovalPolicyRule{
			{
				ID:                 "small-low-risk-withdrawal",
				Purpose:            SigningPurposeWithdrawal,
				AmountMinorLTE:     &lowWithdrawal,
				RiskLevelIn:        []ApprovalRiskLevel{ApprovalRiskLevelLow},
				RequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalRisk},
			},
			{
				ID:                 "manual-treasury-small",
				Purpose:            SigningPurposeTreasury,
				AmountMinorLTE:     &smallTreasury,
				RequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalAdmin},
			},
			{
				ID:                 "high-value",
				AmountMinorGTE:     &highValue,
				RequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalAdmin, ApprovalPrincipalRisk},
			},
		},
		EffectiveFrom: effectiveFrom,
		ExpiresAt:     expiresAt,
	}
}

func (s *MoneyMovementApprovalSigner) BuildArtifacts(ctx context.Context, intent MoneyMovementIntent, adminPrincipalID *id.ID, stepUpProofDigest string) (MoneyMovementApprovalArtifacts, error) {
	var out MoneyMovementApprovalArtifacts
	now := s.now()
	policy := s.signedPolicy.Policy
	if err := policy.Validate(now); err != nil {
		return out, err
	}
	policyJWS := s.signedPolicy.JWS
	policyDigest := s.signedPolicy.Digest

	if err := intent.ValidateSources(); err != nil {
		return out, err
	}
	if intent.IntentID == "" {
		intent.IntentID = id.New().String()
	}
	if intent.PolicyVersion == "" {
		intent.PolicyVersion = policy.PolicyVersion
	} else if intent.PolicyVersion != policy.PolicyVersion {
		return out, fmt.Errorf("intent policy_version %s does not match active policy %s", intent.PolicyVersion, policy.PolicyVersion)
	}
	if intent.PolicyDigest == "" {
		intent.PolicyDigest = policyDigest
	} else if intent.PolicyDigest != policyDigest {
		return out, fmt.Errorf("intent policy_digest does not match active policy")
	}
	if intent.ExpiresAt.IsZero() {
		intent.ExpiresAt = now.Add(DefaultApprovalTTL)
	}
	intentJWS, err := s.jwsSigner.SignApprovalJWS(ctx, intent.TenantID, ApprovalSigningRoleIntent, intent)
	if err != nil {
		return out, fmt.Errorf("sign money movement intent: %w", err)
	}
	intentDigest, err := JWSPayloadDigest(intentJWS)
	if err != nil {
		return out, err
	}

	risk := s.assessRisk(intent, intentDigest, now)
	riskJWS, err := s.jwsSigner.SignApprovalJWS(ctx, intent.TenantID, ApprovalSigningRoleRiskAssessment, risk)
	if err != nil {
		return out, fmt.Errorf("sign risk assessment: %w", err)
	}
	riskDigest, err := JWSPayloadDigest(riskJWS)
	if err != nil {
		return out, err
	}

	required, matched, err := policy.RequiredPrincipals(intent, risk)
	if err != nil {
		return out, err
	}
	decision := ApprovalPolicyDecision{
		SchemaVersion:      PolicyDecisionSchemaV1,
		TenantID:           intent.TenantID,
		IntentDigest:       intentDigest,
		PolicyVersion:      policy.PolicyVersion,
		PolicyDigest:       policyDigest,
		MatchedRuleIDs:     matched,
		RequiredPrincipals: required,
		DecidedAt:          now,
		ExpiresAt:          minTime(intent.ExpiresAt, risk.ExpiresAt),
	}
	decisionJWS, err := s.jwsSigner.SignApprovalJWS(ctx, intent.TenantID, ApprovalSigningRolePolicyDecision, decision)
	if err != nil {
		return out, fmt.Errorf("sign approval policy decision: %w", err)
	}
	decisionDigest, err := JWSPayloadDigest(decisionJWS)
	if err != nil {
		return out, err
	}

	approvals := make([]MoneyMovementApproval, 0, len(required))
	bundleApprovals := make([]ApprovalBundleApproval, 0, len(required))
	for _, principal := range required {
		switch principal {
		case ApprovalPrincipalRisk:
			approval := MoneyMovementApproval{
				SchemaVersion:        MoneyMovementApprovalV1,
				ApprovalID:           id.New().String(),
				TenantID:             intent.TenantID,
				IntentDigest:         intentDigest,
				PrincipalType:        ApprovalPrincipalRisk,
				PrincipalID:          "risk-engine",
				PolicyVersion:        policy.PolicyVersion,
				PolicyDecisionDigest: decisionDigest,
				RiskAssessmentDigest: riskDigest,
				ApprovedAt:           now,
				ExpiresAt:            decision.ExpiresAt,
			}
			jws, err := s.jwsSigner.SignApprovalJWS(ctx, intent.TenantID, ApprovalSigningRoleRiskApproval, approval)
			if err != nil {
				return out, fmt.Errorf("sign risk approval: %w", err)
			}
			approvals = append(approvals, approval)
			bundleApprovals = append(bundleApprovals, ApprovalBundleApproval{PrincipalType: ApprovalPrincipalRisk, JWS: jws})
		case ApprovalPrincipalAdmin:
			if adminPrincipalID == nil || id.IsNil(*adminPrincipalID) {
				continue
			}
			if stepUpProofDigest == "" {
				return out, fmt.Errorf("admin approval requires step-up proof digest")
			}
			approval := MoneyMovementApproval{
				SchemaVersion:        MoneyMovementApprovalV1,
				ApprovalID:           id.New().String(),
				TenantID:             intent.TenantID,
				IntentDigest:         intentDigest,
				PrincipalType:        ApprovalPrincipalAdmin,
				PrincipalID:          adminPrincipalID.String(),
				PolicyVersion:        policy.PolicyVersion,
				PolicyDecisionDigest: decisionDigest,
				StepUpProofDigest:    stepUpProofDigest,
				ApprovedAt:           now,
				ExpiresAt:            decision.ExpiresAt,
			}
			jws, err := s.jwsSigner.SignApprovalJWS(ctx, intent.TenantID, ApprovalSigningRoleAdminApproval, approval)
			if err != nil {
				return out, fmt.Errorf("sign admin approval: %w", err)
			}
			approvals = append(approvals, approval)
			bundleApprovals = append(bundleApprovals, ApprovalBundleApproval{PrincipalType: ApprovalPrincipalAdmin, JWS: jws})
		}
	}

	out.Bundle = ApprovalBundle{
		PolicyBundleJWS:   policyJWS,
		IntentJWS:         intentJWS,
		RiskAssessmentJWS: riskJWS,
		PolicyDecisionJWS: decisionJWS,
		Approvals:         bundleApprovals,
	}
	out.Intent = intent
	out.IntentDigest = intentDigest
	out.RiskAssessment = risk
	out.RiskDigest = riskDigest
	out.PolicyDecision = decision
	out.PolicyDecisionDigest = decisionDigest
	out.Approvals = approvals
	out.RequiredPrincipals = required
	out.MatchedRuleIDs = matched
	return out, nil
}

func (s *MoneyMovementApprovalSigner) BuildAdminApprovalForBundle(ctx context.Context, bundle ApprovalBundle, adminPrincipalID id.ID, stepUpProofDigest string) (MoneyMovementSignedApproval, error) {
	var out MoneyMovementSignedApproval
	if id.IsNil(adminPrincipalID) {
		return out, fmt.Errorf("admin principal id is required")
	}
	if stepUpProofDigest == "" {
		return out, fmt.Errorf("admin approval requires step-up proof digest")
	}
	now := s.now()

	var policy ApprovalPolicyBundle
	policyPayload, err := VerifyEdDSAJWS(bundle.PolicyBundleJWS, s.publicKeys.PolicyPublicKey, &policy)
	if err != nil {
		return out, fmt.Errorf("verify approval policy: %w", err)
	}
	policyDigest := digestBytes(policyPayload)
	if policy.PolicyVersion != s.signedPolicy.Policy.PolicyVersion || policyDigest != s.signedPolicy.Digest {
		return out, fmt.Errorf("stored approval policy does not match active signer policy")
	}
	if err := policy.Validate(now); err != nil {
		return out, err
	}

	var intent MoneyMovementIntent
	intentPayload, err := VerifyEdDSAJWS(bundle.IntentJWS, s.publicKeys.IntentPublicKey, &intent)
	if err != nil {
		return out, fmt.Errorf("verify money movement intent: %w", err)
	}
	intentDigest := digestBytes(intentPayload)
	if err := validateIntent(intent, policy, policyDigest, now); err != nil {
		return out, err
	}

	var risk RiskAssessment
	if _, err := VerifyEdDSAJWS(bundle.RiskAssessmentJWS, s.publicKeys.RiskPublicKey, &risk); err != nil {
		return out, fmt.Errorf("verify risk assessment: %w", err)
	}
	if err := validateRiskAssessment(risk, intent, intentDigest, now); err != nil {
		return out, err
	}

	var decision ApprovalPolicyDecision
	decisionPayload, err := VerifyEdDSAJWS(bundle.PolicyDecisionJWS, s.publicKeys.DecisionPublicKey, &decision)
	if err != nil {
		return out, fmt.Errorf("verify approval policy decision: %w", err)
	}
	decisionDigest := digestBytes(decisionPayload)
	if err := validatePolicyDecision(policy, intent, risk, decision, intentDigest, policyDigest, now); err != nil {
		return out, err
	}
	if !slices.Contains(decision.RequiredPrincipals, ApprovalPrincipalAdmin) {
		return out, fmt.Errorf("approval policy decision does not require admin approval")
	}

	approval := MoneyMovementApproval{
		SchemaVersion:        MoneyMovementApprovalV1,
		ApprovalID:           id.New().String(),
		TenantID:             intent.TenantID,
		IntentDigest:         intentDigest,
		PrincipalType:        ApprovalPrincipalAdmin,
		PrincipalID:          adminPrincipalID.String(),
		PolicyVersion:        policy.PolicyVersion,
		PolicyDecisionDigest: decisionDigest,
		StepUpProofDigest:    stepUpProofDigest,
		ApprovedAt:           now,
		ExpiresAt:            decision.ExpiresAt,
	}
	jws, err := s.jwsSigner.SignApprovalJWS(ctx, intent.TenantID, ApprovalSigningRoleAdminApproval, approval)
	if err != nil {
		return out, fmt.Errorf("sign admin approval: %w", err)
	}
	out.Approval = approval
	out.Bundle = ApprovalBundleApproval{PrincipalType: ApprovalPrincipalAdmin, JWS: jws}
	return out, nil
}

func (s *MoneyMovementApprovalSigner) assessRisk(intent MoneyMovementIntent, intentDigest string, now time.Time) RiskAssessment {
	level := ApprovalRiskLevelLow
	score := 10
	signals := []string{"policy_static_v1", "allowlisted_address"}
	if intent.AmountMinorUnits.Cmp(types.NewRawAmountFromInt64(5_000_000_000)) >= 0 {
		level = ApprovalRiskLevelHigh
		score = 90
		signals = append(signals, "high_value")
	} else if intent.AmountMinorUnits.Cmp(types.NewRawAmountFromInt64(1_000_000_000)) >= 0 {
		level = ApprovalRiskLevelMedium
		score = 50
		signals = append(signals, "medium_value")
	}
	if intent.SchemaVersion == MoneyMovementIntentV2 {
		signals = append(signals, "aml_source_class_"+intent.RiskClass)
		if intent.RiskClass == "high" || intent.RiskClass == "unknown" {
			level = ApprovalRiskLevelHigh
			score = max(score, 90)
		} else if intent.RiskClass == "medium" && level == ApprovalRiskLevelLow {
			level = ApprovalRiskLevelMedium
			score = max(score, 50)
		}
	}
	return RiskAssessment{
		SchemaVersion: RiskAssessmentSchemaV1,
		TenantID:      intent.TenantID,
		IntentDigest:  intentDigest,
		RiskLevel:     level,
		RiskScore:     score,
		Signals:       signals,
		AssessedAt:    now,
		ExpiresAt:     now.Add(DefaultRiskAssessmentTTL),
	}
}

func ValidateApprovalPublicKeys(keys ApprovalPublicKeys) error {
	publicKeys := []struct {
		name string
		key  ed25519.PublicKey
	}{
		{"policy", keys.PolicyPublicKey},
		{"intent", keys.IntentPublicKey},
		{"risk_assessment", keys.RiskPublicKey},
		{"policy_decision", keys.DecisionPublicKey},
		{"admin_approval", keys.AdminApprovalPublicKey},
		{"risk_approval", keys.RiskApprovalPublicKey},
	}
	seen := make(map[string]string, len(publicKeys))
	for _, item := range publicKeys {
		if len(item.key) != ed25519.PublicKeySize {
			return fmt.Errorf("%w: %s public key must be %d bytes", ErrInvalidSigningKey, item.name, ed25519.PublicKeySize)
		}
		fingerprint := string(item.key)
		if previous, ok := seen[fingerprint]; ok {
			return fmt.Errorf("%w: %s public key duplicates %s public key", ErrInvalidSigningKey, item.name, previous)
		}
		seen[fingerprint] = item.name
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() || a.Before(b) {
		return a
	}
	return b
}
