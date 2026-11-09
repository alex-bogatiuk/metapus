package crypto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

// ErrApprovalArtifactExpired distinguishes an expected pre-signing timeout
// from tampering, policy mismatch, or an invalid signature. Callers may safely
// compensate an unbroadcast financial command instead of placing it in a
// manual security hold.
var ErrApprovalArtifactExpired = errors.New("money movement approval artifact expired")

const (
	ApprovalPolicySchemaV1   = "approval_policy.v1"
	MoneyMovementIntentV1    = "money_movement_intent.v1"
	MoneyMovementIntentV2    = "money_movement_intent.v2"
	RiskAssessmentSchemaV1   = "risk_assessment.v1"
	PolicyDecisionSchemaV1   = "approval_policy_decision.v1"
	MoneyMovementApprovalV1  = "money_movement_approval.v1"
	DefaultApprovalPolicyV1  = "money-movement-v1.0"
	DefaultApprovalTTL       = 10 * time.Minute
	DefaultRiskAssessmentTTL = 10 * time.Minute
)

type ApprovalPrincipal string

const (
	ApprovalPrincipalAdmin ApprovalPrincipal = "admin"
	ApprovalPrincipalRisk  ApprovalPrincipal = "risk"
)

type ApprovalRiskLevel string

const (
	ApprovalRiskLevelLow    ApprovalRiskLevel = "low"
	ApprovalRiskLevelMedium ApprovalRiskLevel = "medium"
	ApprovalRiskLevelHigh   ApprovalRiskLevel = "high"
)

// ApprovalPolicyBundle is signed by platform security and is not sourced from
// the tenant database. It defines which independent principals must approve a
// money movement intent.
type ApprovalPolicyBundle struct {
	SchemaVersion             string               `json:"schema_version"`
	PolicyVersion             string               `json:"policy_version"`
	TenantID                  string               `json:"tenant_id"`
	DefaultRequiredPrincipals []ApprovalPrincipal  `json:"default_required_principals"`
	Rules                     []ApprovalPolicyRule `json:"rules"`
	EffectiveFrom             time.Time            `json:"effective_from"`
	ExpiresAt                 time.Time            `json:"expires_at"`
}

type ApprovalPolicyRule struct {
	ID                     string              `json:"id"`
	Purpose                SigningPurpose      `json:"purpose,omitempty"`
	AmountMinorLTE         *types.CryptoAmount `json:"amount_minor_lte,omitempty"`
	AmountMinorGTE         *types.CryptoAmount `json:"amount_minor_gte,omitempty"`
	NetworkCode            string              `json:"network_code,omitempty"`
	TokenID                string              `json:"token_id,omitempty"`
	RiskLevelIn            []ApprovalRiskLevel `json:"risk_level_in,omitempty"`
	DestinationMinAgeHours *int                `json:"destination_min_age_hours,omitempty"`
	RequiredPrincipals     []ApprovalPrincipal `json:"required_principals"`
}

type MoneyMovementIntent struct {
	SchemaVersion      string                    `json:"schema_version"`
	IntentID           string                    `json:"intent_id"`
	TenantID           string                    `json:"tenant_id"`
	Purpose            SigningPurpose            `json:"purpose"`
	BasisType          string                    `json:"basis_type"`
	BasisID            id.ID                     `json:"basis_id"`
	MerchantID         *id.ID                    `json:"merchant_id,omitempty"`
	NetworkCode        string                    `json:"network_code"`
	TokenID            id.ID                     `json:"token_id"`
	TokenStandard      string                    `json:"token_standard"`
	ContractAddress    string                    `json:"contract_address"`
	AmountMinorUnits   types.CryptoAmount        `json:"amount_minor_units"`
	DestinationAddress string                    `json:"destination_address"`
	AddressID          id.ID                     `json:"address_id"`
	SourceAddress      string                    `json:"source_address,omitempty"`
	SourceVaultKeyID   string                    `json:"source_vault_key_id,omitempty"`
	RiskClass          string                    `json:"risk_class,omitempty"`
	RiskAccepted       bool                      `json:"risk_accepted,omitzero"`
	SourceLines        []MoneyMovementSourceLine `json:"source_lines,omitempty"`
	PolicyVersion      string                    `json:"policy_version"`
	PolicyDigest       string                    `json:"policy_digest"`
	ExpiresAt          time.Time                 `json:"expires_at"`
}

// MoneyMovementSourceLine is part of the merchant-approved intent. Each line
// authorizes one exact source/outflow; its technical revisions cannot be edited.
type MoneyMovementSourceLine struct {
	LineID           id.ID              `json:"line_id"`
	WalletID         id.ID              `json:"wallet_id"`
	SourceAddress    string             `json:"source_address"`
	SourceVaultKeyID string             `json:"source_vault_key_id"`
	AmountMinorUnits types.CryptoAmount `json:"amount_minor_units"`
	RiskRevision     int64              `json:"risk_revision"`
	ScopeVersion     int64              `json:"scope_version"`
	EvidenceHash     string             `json:"evidence_hash"`
}

func (intent MoneyMovementIntent) ValidateSources() error {
	if intent.SchemaVersion == MoneyMovementIntentV1 {
		if intent.BasisType == "CryptoAMLWithdrawalLine" || intent.RiskClass != "" || intent.RiskAccepted || len(intent.SourceLines) > 0 {
			return fmt.Errorf("risk source contract requires intent v2")
		}
		return nil
	}
	if intent.SchemaVersion != MoneyMovementIntentV2 || intent.Purpose != SigningPurposeWithdrawal || intent.BasisType != "WithdrawalRequest" || len(intent.SourceLines) == 0 || len(intent.SourceLines) > 1000 {
		return fmt.Errorf("invalid risk withdrawal source contract")
	}
	if intent.RiskClass != "low" && intent.RiskClass != "medium" && intent.RiskClass != "high" && intent.RiskClass != "unknown" {
		return fmt.Errorf("risk class is required")
	}
	if (intent.RiskClass == "high" || intent.RiskClass == "unknown") && !intent.RiskAccepted {
		return fmt.Errorf("explicit risk acceptance is required")
	}
	total := types.ZeroCryptoAmount()
	ids, wallets := map[id.ID]bool{}, map[id.ID]bool{}
	for _, line := range intent.SourceLines {
		if id.IsNil(line.LineID) || id.IsNil(line.WalletID) || ids[line.LineID] || wallets[line.WalletID] || line.SourceAddress == "" || line.SourceVaultKeyID == "" || !line.AmountMinorUnits.IsPositive() || line.RiskRevision <= 0 || line.ScopeVersion <= 0 {
			return fmt.Errorf("invalid or duplicate approved source line")
		}
		ids[line.LineID], wallets[line.WalletID] = true, true
		total = total.Add(line.AmountMinorUnits)
	}
	if !total.Equal(intent.AmountMinorUnits) {
		return fmt.Errorf("approved source total differs from withdrawal amount")
	}
	return nil
}

func (intent MoneyMovementIntent) AuthorizesSource(basis string, basisID id.ID, address, key string, amount types.CryptoAmount) bool {
	if intent.SchemaVersion == MoneyMovementIntentV1 {
		return intent.BasisType == basis && intent.BasisID == basisID && intent.AmountMinorUnits.Equal(amount)
	}
	if intent.ValidateSources() != nil || basis != "CryptoAMLWithdrawalLine" {
		return false
	}
	for _, line := range intent.SourceLines {
		if line.LineID == basisID && line.SourceAddress == address && line.SourceVaultKeyID == key && line.AmountMinorUnits.Equal(amount) {
			return true
		}
	}
	return false
}

type RiskAssessment struct {
	SchemaVersion       string            `json:"schema_version"`
	TenantID            string            `json:"tenant_id"`
	IntentDigest        string            `json:"intent_digest"`
	RiskLevel           ApprovalRiskLevel `json:"risk_level"`
	RiskScore           int               `json:"risk_score"`
	Signals             []string          `json:"signals"`
	DestinationAgeHours *int              `json:"destination_age_hours,omitempty"`
	AssessedAt          time.Time         `json:"assessed_at"`
	ExpiresAt           time.Time         `json:"expires_at"`
}

type ApprovalPolicyDecision struct {
	SchemaVersion      string              `json:"schema_version"`
	TenantID           string              `json:"tenant_id"`
	IntentDigest       string              `json:"intent_digest"`
	PolicyVersion      string              `json:"policy_version"`
	PolicyDigest       string              `json:"policy_digest"`
	MatchedRuleIDs     []string            `json:"matched_rule_ids"`
	RequiredPrincipals []ApprovalPrincipal `json:"required_principals"`
	DecidedAt          time.Time           `json:"decided_at"`
	ExpiresAt          time.Time           `json:"expires_at"`
}

type MoneyMovementApproval struct {
	SchemaVersion        string            `json:"schema_version"`
	ApprovalID           string            `json:"approval_id"`
	TenantID             string            `json:"tenant_id"`
	IntentDigest         string            `json:"intent_digest"`
	PrincipalType        ApprovalPrincipal `json:"principal_type"`
	PrincipalID          string            `json:"principal_id"`
	PolicyVersion        string            `json:"policy_version"`
	PolicyDecisionDigest string            `json:"policy_decision_digest"`
	RiskAssessmentDigest string            `json:"risk_assessment_digest,omitempty"`
	StepUpProofDigest    string            `json:"step_up_proof_digest,omitempty"`
	ApprovedAt           time.Time         `json:"approved_at"`
	ExpiresAt            time.Time         `json:"expires_at"`
}

type ApprovalBundleApproval struct {
	PrincipalType ApprovalPrincipal `json:"principal_type"`
	JWS           string            `json:"jws"`
}

// ApprovalBundle is sent to Vault with the worker authorization and unsigned
// transaction. All JWS payloads must bind to the same intent digest.
type ApprovalBundle struct {
	PolicyBundleJWS   string                   `json:"policy_bundle_jws"`
	IntentJWS         string                   `json:"intent_jws"`
	RiskAssessmentJWS string                   `json:"risk_assessment_jws"`
	PolicyDecisionJWS string                   `json:"policy_decision_jws"`
	Approvals         []ApprovalBundleApproval `json:"approvals"`
}

type ApprovalPublicKeys struct {
	PolicyPublicKey        ed25519.PublicKey
	IntentPublicKey        ed25519.PublicKey
	RiskPublicKey          ed25519.PublicKey
	DecisionPublicKey      ed25519.PublicKey
	AdminApprovalPublicKey ed25519.PublicKey
	RiskApprovalPublicKey  ed25519.PublicKey
}

type VerifiedApprovalBundle struct {
	Policy               ApprovalPolicyBundle
	Intent               MoneyMovementIntent
	RiskAssessment       RiskAssessment
	Decision             ApprovalPolicyDecision
	Approvals            []MoneyMovementApproval
	IntentDigest         string
	PolicyDigest         string
	RiskAssessmentDigest string
	PolicyDecisionDigest string
	BundleDigest         string
}

func (p ApprovalPolicyBundle) Validate(now time.Time) error {
	if p.SchemaVersion != ApprovalPolicySchemaV1 {
		return fmt.Errorf("unsupported approval policy schema_version: %s", p.SchemaVersion)
	}
	if strings.TrimSpace(p.PolicyVersion) == "" {
		return fmt.Errorf("approval policy_version is required")
	}
	if strings.TrimSpace(p.TenantID) == "" {
		return fmt.Errorf("approval policy tenant_id is required")
	}
	if !p.EffectiveFrom.IsZero() && now.Before(p.EffectiveFrom) {
		return fmt.Errorf("approval policy is not effective yet")
	}
	if p.ExpiresAt.IsZero() || !now.Before(p.ExpiresAt) {
		return fmt.Errorf("approval policy expired")
	}
	if _, err := normalizePrincipals(p.DefaultRequiredPrincipals); err != nil {
		return fmt.Errorf("invalid default required principals: %w", err)
	}
	for _, rule := range p.Rules {
		if strings.TrimSpace(rule.ID) == "" {
			return fmt.Errorf("approval policy rule id is required")
		}
		if _, err := normalizePrincipals(rule.RequiredPrincipals); err != nil {
			return fmt.Errorf("invalid required principals for rule %s: %w", rule.ID, err)
		}
	}
	return nil
}

func (p ApprovalPolicyBundle) RequiredPrincipals(intent MoneyMovementIntent, risk RiskAssessment) ([]ApprovalPrincipal, []string, error) {
	var required []ApprovalPrincipal
	matched := make([]string, 0, len(p.Rules))
	for _, rule := range p.Rules {
		if !rule.matches(intent, risk) {
			continue
		}
		matched = append(matched, rule.ID)
		rulePrincipals, err := normalizePrincipals(rule.RequiredPrincipals)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid principals for rule %s: %w", rule.ID, err)
		}
		required = unionPrincipals(required, rulePrincipals)
	}
	if len(matched) == 0 {
		var err error
		required, err = normalizePrincipals(p.DefaultRequiredPrincipals)
		if err != nil {
			return nil, nil, err
		}
	}
	return required, matched, nil
}

func (r ApprovalPolicyRule) matches(intent MoneyMovementIntent, risk RiskAssessment) bool {
	if r.Purpose != "" && r.Purpose != intent.Purpose {
		return false
	}
	if r.NetworkCode != "" && r.NetworkCode != intent.NetworkCode {
		return false
	}
	if r.TokenID != "" && r.TokenID != intent.TokenID.String() {
		return false
	}
	if r.AmountMinorLTE != nil && intent.AmountMinorUnits.Cmp(*r.AmountMinorLTE) > 0 {
		return false
	}
	if r.AmountMinorGTE != nil && intent.AmountMinorUnits.Cmp(*r.AmountMinorGTE) < 0 {
		return false
	}
	if len(r.RiskLevelIn) > 0 && !slices.Contains(r.RiskLevelIn, risk.RiskLevel) {
		return false
	}
	if r.DestinationMinAgeHours != nil {
		if risk.DestinationAgeHours == nil || *risk.DestinationAgeHours < *r.DestinationMinAgeHours {
			return false
		}
	}
	return true
}

func VerifyApprovalBundle(bundle ApprovalBundle, keys ApprovalPublicKeys, now time.Time) (VerifiedApprovalBundle, error) {
	var verified VerifiedApprovalBundle
	if bundle.PolicyBundleJWS == "" || bundle.IntentJWS == "" || bundle.RiskAssessmentJWS == "" || bundle.PolicyDecisionJWS == "" {
		return verified, fmt.Errorf("approval bundle is incomplete")
	}

	policyPayload, err := VerifyEdDSAJWS(bundle.PolicyBundleJWS, keys.PolicyPublicKey, &verified.Policy)
	if err != nil {
		return verified, fmt.Errorf("verify approval policy: %w", err)
	}
	verified.PolicyDigest = digestBytes(policyPayload)
	if err := verified.Policy.Validate(now); err != nil {
		return verified, err
	}

	intentPayload, err := VerifyEdDSAJWS(bundle.IntentJWS, keys.IntentPublicKey, &verified.Intent)
	if err != nil {
		return verified, fmt.Errorf("verify money movement intent: %w", err)
	}
	verified.IntentDigest = digestBytes(intentPayload)
	if err := validateIntent(verified.Intent, verified.Policy, verified.PolicyDigest, now); err != nil {
		return verified, err
	}

	riskPayload, err := VerifyEdDSAJWS(bundle.RiskAssessmentJWS, keys.RiskPublicKey, &verified.RiskAssessment)
	if err != nil {
		return verified, fmt.Errorf("verify risk assessment: %w", err)
	}
	verified.RiskAssessmentDigest = digestBytes(riskPayload)
	if err := validateRiskAssessment(verified.RiskAssessment, verified.Intent, verified.IntentDigest, now); err != nil {
		return verified, err
	}

	decisionPayload, err := VerifyEdDSAJWS(bundle.PolicyDecisionJWS, keys.DecisionPublicKey, &verified.Decision)
	if err != nil {
		return verified, fmt.Errorf("verify approval policy decision: %w", err)
	}
	verified.PolicyDecisionDigest = digestBytes(decisionPayload)
	if err := validatePolicyDecision(verified.Policy, verified.Intent, verified.RiskAssessment, verified.Decision, verified.IntentDigest, verified.PolicyDigest, now); err != nil {
		return verified, err
	}

	approvals, err := verifyApprovals(bundle.Approvals, keys, verified.Decision, verified.IntentDigest, verified.RiskAssessmentDigest, verified.PolicyDecisionDigest, now)
	if err != nil {
		return verified, err
	}
	verified.Approvals = approvals
	if err := ensureRequiredApprovals(verified.Decision.RequiredPrincipals, approvals); err != nil {
		return verified, err
	}
	verified.BundleDigest, err = DigestJSON(bundle)
	if err != nil {
		return verified, err
	}
	return verified, nil
}

func validateIntent(intent MoneyMovementIntent, policy ApprovalPolicyBundle, policyDigest string, now time.Time) error {
	if intent.SchemaVersion != MoneyMovementIntentV1 && intent.SchemaVersion != MoneyMovementIntentV2 {
		return fmt.Errorf("unsupported money movement intent schema_version: %s", intent.SchemaVersion)
	}
	if err := intent.ValidateSources(); err != nil {
		return err
	}
	if intent.TenantID != policy.TenantID {
		return fmt.Errorf("intent tenant_id does not match policy")
	}
	if intent.PolicyVersion != policy.PolicyVersion || intent.PolicyDigest != policyDigest {
		return fmt.Errorf("intent policy does not match signed policy bundle")
	}
	if intent.ExpiresAt.IsZero() || !now.Before(intent.ExpiresAt) {
		return fmt.Errorf("%w: intent", ErrApprovalArtifactExpired)
	}
	if !intent.Purpose.RequiresApprovalBundle() {
		return fmt.Errorf("unsupported money movement purpose: %s", intent.Purpose)
	}
	if id.IsNil(intent.BasisID) || id.IsNil(intent.TokenID) || id.IsNil(intent.AddressID) {
		return fmt.Errorf("money movement intent has empty required ids")
	}
	if !intent.AmountMinorUnits.IsPositive() {
		return fmt.Errorf("money movement intent amount must be positive")
	}
	if intent.DestinationAddress == "" || intent.NetworkCode == "" || intent.TokenStandard == "" {
		return fmt.Errorf("money movement intent is missing transaction fields")
	}
	if intent.Purpose == SigningPurposeTreasury && (intent.SourceAddress == "" || intent.SourceVaultKeyID == "") {
		return fmt.Errorf("treasury intent requires source wallet binding")
	}
	return nil
}

func validateRiskAssessment(risk RiskAssessment, intent MoneyMovementIntent, intentDigest string, now time.Time) error {
	if risk.SchemaVersion != RiskAssessmentSchemaV1 {
		return fmt.Errorf("unsupported risk assessment schema_version: %s", risk.SchemaVersion)
	}
	if risk.TenantID != intent.TenantID || risk.IntentDigest != intentDigest {
		return fmt.Errorf("risk assessment does not match intent")
	}
	if risk.ExpiresAt.IsZero() || !now.Before(risk.ExpiresAt) {
		return fmt.Errorf("%w: risk assessment", ErrApprovalArtifactExpired)
	}
	if intent.SchemaVersion == MoneyMovementIntentV2 && (((intent.RiskClass == "high" || intent.RiskClass == "unknown") && risk.RiskLevel != ApprovalRiskLevelHigh) || (intent.RiskClass == "medium" && risk.RiskLevel == ApprovalRiskLevelLow)) {
		return fmt.Errorf("approval risk cannot understate the selected asset class")
	}
	switch risk.RiskLevel {
	case ApprovalRiskLevelLow, ApprovalRiskLevelMedium, ApprovalRiskLevelHigh:
		return nil
	default:
		return fmt.Errorf("invalid risk level: %s", risk.RiskLevel)
	}
}

func validatePolicyDecision(policy ApprovalPolicyBundle, intent MoneyMovementIntent, risk RiskAssessment, decision ApprovalPolicyDecision, intentDigest, policyDigest string, now time.Time) error {
	if decision.SchemaVersion != PolicyDecisionSchemaV1 {
		return fmt.Errorf("unsupported policy decision schema_version: %s", decision.SchemaVersion)
	}
	if decision.TenantID != intent.TenantID || decision.IntentDigest != intentDigest {
		return fmt.Errorf("policy decision does not match intent")
	}
	if decision.PolicyVersion != policy.PolicyVersion || decision.PolicyDigest != policyDigest {
		return fmt.Errorf("policy decision does not match policy")
	}
	if decision.ExpiresAt.IsZero() || !now.Before(decision.ExpiresAt) {
		return fmt.Errorf("%w: policy decision", ErrApprovalArtifactExpired)
	}
	required, matched, err := policy.RequiredPrincipals(intent, risk)
	if err != nil {
		return err
	}
	decisionRequired, err := normalizePrincipals(decision.RequiredPrincipals)
	if err != nil {
		return err
	}
	if !slices.Equal(required, decisionRequired) {
		return fmt.Errorf("policy decision required principals do not match policy")
	}
	slices.Sort(matched)
	slices.Sort(decision.MatchedRuleIDs)
	if !slices.Equal(matched, decision.MatchedRuleIDs) {
		return fmt.Errorf("policy decision matched rules do not match policy")
	}
	return nil
}

func verifyApprovals(items []ApprovalBundleApproval, keys ApprovalPublicKeys, decision ApprovalPolicyDecision, intentDigest, riskDigest, decisionDigest string, now time.Time) ([]MoneyMovementApproval, error) {
	approvals := make([]MoneyMovementApproval, 0, len(items))
	for _, item := range items {
		var key ed25519.PublicKey
		switch item.PrincipalType {
		case ApprovalPrincipalAdmin:
			key = keys.AdminApprovalPublicKey
		case ApprovalPrincipalRisk:
			key = keys.RiskApprovalPublicKey
		default:
			return nil, fmt.Errorf("unsupported approval principal: %s", item.PrincipalType)
		}
		var approval MoneyMovementApproval
		if _, err := VerifyEdDSAJWS(item.JWS, key, &approval); err != nil {
			return nil, fmt.Errorf("verify %s approval: %w", item.PrincipalType, err)
		}
		if approval.SchemaVersion != MoneyMovementApprovalV1 {
			return nil, fmt.Errorf("unsupported approval schema_version: %s", approval.SchemaVersion)
		}
		if approval.PrincipalType != item.PrincipalType {
			return nil, fmt.Errorf("approval principal type mismatch")
		}
		if approval.TenantID != decision.TenantID || approval.IntentDigest != intentDigest {
			return nil, fmt.Errorf("approval does not match intent")
		}
		if approval.PolicyVersion != decision.PolicyVersion || approval.PolicyDecisionDigest != decisionDigest {
			return nil, fmt.Errorf("approval does not match policy decision")
		}
		if approval.ExpiresAt.IsZero() || !now.Before(approval.ExpiresAt) {
			return nil, fmt.Errorf("%w: %s approval", ErrApprovalArtifactExpired, item.PrincipalType)
		}
		if approval.PrincipalType == ApprovalPrincipalAdmin && approval.StepUpProofDigest == "" {
			return nil, fmt.Errorf("admin approval requires step_up_proof_digest")
		}
		if approval.PrincipalType == ApprovalPrincipalRisk && approval.RiskAssessmentDigest != riskDigest {
			return nil, fmt.Errorf("risk approval risk_assessment_digest does not match risk assessment")
		}
		approvals = append(approvals, approval)
	}
	return approvals, nil
}

func ensureRequiredApprovals(required []ApprovalPrincipal, approvals []MoneyMovementApproval) error {
	seen := make(map[ApprovalPrincipal]bool, len(approvals))
	for _, approval := range approvals {
		seen[approval.PrincipalType] = true
	}
	for _, principal := range required {
		if !seen[principal] {
			return fmt.Errorf("missing required %s approval", principal)
		}
	}
	return nil
}

func normalizePrincipals(in []ApprovalPrincipal) ([]ApprovalPrincipal, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("required principals cannot be empty")
	}
	seen := make(map[ApprovalPrincipal]bool, len(in))
	out := make([]ApprovalPrincipal, 0, len(in))
	for _, p := range in {
		switch p {
		case ApprovalPrincipalAdmin, ApprovalPrincipalRisk:
		default:
			return nil, fmt.Errorf("unsupported principal: %s", p)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	slices.Sort(out)
	return out, nil
}

func unionPrincipals(left, right []ApprovalPrincipal) []ApprovalPrincipal {
	seen := make(map[ApprovalPrincipal]bool, len(left)+len(right))
	out := make([]ApprovalPrincipal, 0, len(left)+len(right))
	for _, p := range append(left, right...) {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func digestBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func DigestJSON(v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return digestBytes(payload), nil
}
