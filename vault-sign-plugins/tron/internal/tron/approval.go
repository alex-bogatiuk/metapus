package tron

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"
	"vault-plugin-secrets-tron/internal/json"
)

type ApprovalPrincipal string

const (
	ApprovalPrincipalAdmin ApprovalPrincipal = "admin"
	ApprovalPrincipalRisk  ApprovalPrincipal = "risk"
)

type RiskLevel string

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
	ID                 string              `json:"id"`
	Purpose            string              `json:"purpose,omitempty"`
	AmountMinorLTE     *RawAmount          `json:"amount_minor_lte,omitempty"`
	AmountMinorGTE     *RawAmount          `json:"amount_minor_gte,omitempty"`
	NetworkCode        string              `json:"network_code,omitempty"`
	TokenID            string              `json:"token_id,omitempty"`
	RiskLevelIn        []RiskLevel         `json:"risk_level_in,omitempty"`
	RequiredPrincipals []ApprovalPrincipal `json:"required_principals"`
}

type MoneyMovementIntent struct {
	SchemaVersion      string                    `json:"schema_version"`
	IntentID           string                    `json:"intent_id"`
	TenantID           string                    `json:"tenant_id"`
	Purpose            string                    `json:"purpose"`
	BasisType          string                    `json:"basis_type"`
	BasisID            string                    `json:"basis_id"`
	MerchantID         *string                   `json:"merchant_id,omitempty"`
	NetworkCode        string                    `json:"network_code"`
	TokenID            string                    `json:"token_id"`
	TokenStandard      string                    `json:"token_standard"`
	ContractAddress    string                    `json:"contract_address"`
	AmountMinorUnits   RawAmount                 `json:"amount_minor_units"`
	DestinationAddress string                    `json:"destination_address"`
	AddressID          string                    `json:"address_id"`
	SourceAddress      string                    `json:"source_address,omitempty"`
	SourceVaultKeyID   string                    `json:"source_vault_key_id,omitempty"`
	RiskClass          string                    `json:"risk_class,omitempty"`
	RiskAccepted       bool                      `json:"risk_accepted,omitzero"`
	SourceLines        []MoneyMovementSourceLine `json:"source_lines,omitempty"`
	PolicyVersion      string                    `json:"policy_version"`
	PolicyDigest       string                    `json:"policy_digest"`
	ExpiresAt          time.Time                 `json:"expires_at"`
}

type MoneyMovementSourceLine struct {
	LineID           string    `json:"line_id"`
	WalletID         string    `json:"wallet_id"`
	SourceAddress    string    `json:"source_address"`
	SourceVaultKeyID string    `json:"source_vault_key_id"`
	AmountMinorUnits RawAmount `json:"amount_minor_units"`
	RiskRevision     int64     `json:"risk_revision"`
	ScopeVersion     int64     `json:"scope_version"`
	EvidenceHash     string    `json:"evidence_hash"`
}

func (intent MoneyMovementIntent) authorizesSource(auth SigningAuthorization) bool {
	if intent.SchemaVersion == "money_movement_intent.v1" {
		return auth.BasisType != "CryptoAMLWithdrawalLine" && intent.RiskClass == "" && !intent.RiskAccepted && len(intent.SourceLines) == 0 && intent.BasisType == auth.BasisType && intent.BasisID == auth.BasisID && intent.AmountMinorUnits.Equal(auth.AmountMinorUnits)
	}
	if intent.SchemaVersion != "money_movement_intent.v2" || intent.Purpose != "withdrawal" || intent.BasisType != "WithdrawalRequest" || intent.BasisID == "" || auth.BasisType != "CryptoAMLWithdrawalLine" || auth.Attempt != 1 || auth.Generation != 1 || len(intent.SourceLines) == 0 || len(intent.SourceLines) > 1000 {
		return false
	}
	if intent.RiskClass != "low" && intent.RiskClass != "medium" && intent.RiskClass != "high" && intent.RiskClass != "unknown" {
		return false
	}
	if (intent.RiskClass == "high" || intent.RiskClass == "unknown") && !intent.RiskAccepted {
		return false
	}
	total := new(big.Int)
	ids, wallets := map[string]bool{}, map[string]bool{}
	matches := false
	for _, line := range intent.SourceLines {
		if line.LineID == "" || line.WalletID == "" || ids[line.LineID] || wallets[line.WalletID] || line.SourceAddress == "" || line.SourceVaultKeyID == "" || !line.AmountMinorUnits.IsPositive() || line.RiskRevision <= 0 || line.ScopeVersion <= 0 {
			return false
		}
		ids[line.LineID], wallets[line.WalletID] = true, true
		amount, err := line.AmountMinorUnits.BigInt()
		if err != nil {
			return false
		}
		total.Add(total, amount)
		if line.LineID == auth.BasisID && line.SourceAddress == auth.SourceAddress && line.SourceVaultKeyID == auth.VaultKeyID && line.AmountMinorUnits.Equal(auth.AmountMinorUnits) {
			matches = true
		}
	}
	expected, err := intent.AmountMinorUnits.BigInt()
	return err == nil && total.Cmp(expected) == 0 && matches
}

type RiskAssessment struct {
	SchemaVersion       string    `json:"schema_version"`
	TenantID            string    `json:"tenant_id"`
	IntentDigest        string    `json:"intent_digest"`
	RiskLevel           RiskLevel `json:"risk_level"`
	RiskScore           int       `json:"risk_score"`
	Signals             []string  `json:"signals"`
	DestinationAgeHours *int      `json:"destination_age_hours,omitempty"`
	AssessedAt          time.Time `json:"assessed_at"`
	ExpiresAt           time.Time `json:"expires_at"`
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
			return fmt.Errorf("%s public key must be %d bytes", item.name, ed25519.PublicKeySize)
		}
		fingerprint := string(item.key)
		if previous, ok := seen[fingerprint]; ok {
			return fmt.Errorf("%s public key duplicates %s public key", item.name, previous)
		}
		seen[fingerprint] = item.name
	}
	return nil
}

func ValidateApprovalBundle(bundle ApprovalBundle, keys ApprovalPublicKeys, auth SigningAuthorization, now time.Time) error {
	if auth.Purpose != "withdrawal" && auth.Purpose != "treasury_withdrawal" {
		return nil
	}
	if auth.ApprovalBundleDigest == "" {
		return fmt.Errorf("authorization missing approval_bundle_digest")
	}
	bundleDigest, err := digestJSON(bundle)
	if err != nil {
		return fmt.Errorf("digest approval bundle: %w", err)
	}
	if bundleDigest != auth.ApprovalBundleDigest {
		return fmt.Errorf("approval bundle digest does not match authorization")
	}
	var policy ApprovalPolicyBundle
	policyPayload, err := verifyEdDSAJWS(bundle.PolicyBundleJWS, keys.PolicyPublicKey, &policy)
	if err != nil {
		return fmt.Errorf("verify approval policy: %w", err)
	}
	policyDigest := digestBytes(policyPayload)
	if policy.SchemaVersion != "approval_policy.v1" || policy.TenantID != auth.TenantID || policy.PolicyVersion == "" {
		return fmt.Errorf("approval policy does not match authorization tenant")
	}
	if policy.ExpiresAt.IsZero() || !now.Before(policy.ExpiresAt) {
		return fmt.Errorf("approval policy expired")
	}

	var intent MoneyMovementIntent
	intentPayload, err := verifyEdDSAJWS(bundle.IntentJWS, keys.IntentPublicKey, &intent)
	if err != nil {
		return fmt.Errorf("verify money movement intent: %w", err)
	}
	intentDigest := digestBytes(intentPayload)
	if err := validateIntentAgainstAuthorization(intent, auth, policy, policyDigest, now); err != nil {
		return err
	}

	var risk RiskAssessment
	riskPayload, err := verifyEdDSAJWS(bundle.RiskAssessmentJWS, keys.RiskPublicKey, &risk)
	if err != nil {
		return fmt.Errorf("verify risk assessment: %w", err)
	}
	riskDigest := digestBytes(riskPayload)
	if intent.SchemaVersion == "money_movement_intent.v2" && (((intent.RiskClass == "high" || intent.RiskClass == "unknown") && risk.RiskLevel != "high") || (intent.RiskClass == "medium" && risk.RiskLevel == "low")) {
		return fmt.Errorf("approval risk understates selected asset class")
	}

	if risk.TenantID != auth.TenantID || risk.IntentDigest != intentDigest || risk.ExpiresAt.IsZero() || !now.Before(risk.ExpiresAt) {
		return fmt.Errorf("risk assessment does not match intent")
	}

	var decision ApprovalPolicyDecision
	decisionPayload, err := verifyEdDSAJWS(bundle.PolicyDecisionJWS, keys.DecisionPublicKey, &decision)
	if err != nil {
		return fmt.Errorf("verify approval policy decision: %w", err)
	}
	decisionDigest := digestBytes(decisionPayload)
	required, matched, err := requiredPrincipals(policy, intent, risk)
	if err != nil {
		return err
	}
	decisionRequired, err := normalizePrincipals(decision.RequiredPrincipals)
	if err != nil {
		return err
	}
	slices.Sort(matched)
	slices.Sort(decision.MatchedRuleIDs)
	if decision.TenantID != auth.TenantID || decision.IntentDigest != intentDigest ||
		decision.PolicyVersion != policy.PolicyVersion || decision.PolicyDigest != policyDigest ||
		decision.ExpiresAt.IsZero() || !now.Before(decision.ExpiresAt) ||
		!slices.Equal(required, decisionRequired) || !slices.Equal(matched, decision.MatchedRuleIDs) {
		return fmt.Errorf("policy decision does not match approval policy")
	}

	seen := make(map[ApprovalPrincipal]bool, len(bundle.Approvals))
	for _, item := range bundle.Approvals {
		var key ed25519.PublicKey
		switch item.PrincipalType {
		case ApprovalPrincipalAdmin:
			key = keys.AdminApprovalPublicKey
		case ApprovalPrincipalRisk:
			key = keys.RiskApprovalPublicKey
		default:
			return fmt.Errorf("unsupported approval principal: %s", item.PrincipalType)
		}
		var approval MoneyMovementApproval
		if _, err := verifyEdDSAJWS(item.JWS, key, &approval); err != nil {
			return fmt.Errorf("verify %s approval: %w", item.PrincipalType, err)
		}
		if approval.SchemaVersion != "money_movement_approval.v1" || approval.PrincipalType != item.PrincipalType ||
			approval.TenantID != auth.TenantID || approval.IntentDigest != intentDigest ||
			approval.PolicyVersion != policy.PolicyVersion || approval.PolicyDecisionDigest != decisionDigest ||
			approval.ExpiresAt.IsZero() || !now.Before(approval.ExpiresAt) {
			return fmt.Errorf("%s approval does not match policy decision", item.PrincipalType)
		}
		if approval.PrincipalType == ApprovalPrincipalAdmin && approval.StepUpProofDigest == "" {
			return fmt.Errorf("admin approval missing step_up_proof_digest")
		}
		if approval.PrincipalType == ApprovalPrincipalRisk && approval.RiskAssessmentDigest != riskDigest {
			return fmt.Errorf("risk approval risk_assessment_digest does not match risk assessment")
		}
		seen[approval.PrincipalType] = true
	}
	for _, principal := range required {
		if !seen[principal] {
			return fmt.Errorf("missing required %s approval", principal)
		}
	}
	return nil
}

// ValidateApprovalBundleAndGetIntentDigest validates the complete approval
// chain and returns the SHA-256 digest of the exact IntentJWS payload bytes.
func ValidateApprovalBundleAndGetIntentDigest(bundle ApprovalBundle, keys ApprovalPublicKeys, auth SigningAuthorization, now time.Time) (string, error) {
	if err := ValidateApprovalBundle(bundle, keys, auth, now); err != nil {
		return "", err
	}
	if auth.Purpose != "withdrawal" && auth.Purpose != "treasury_withdrawal" {
		return "", nil
	}
	return IntentJWSPayloadDigest(bundle.IntentJWS)
}

// ApprovalBundleDigest returns the canonical digest bound into a signing
// authorization. It is safe to compare before temporal validation when
// looking up an already-consumed, immutable intent.
func ApprovalBundleDigest(bundle ApprovalBundle) (string, error) {
	return digestJSON(bundle)
}

// IntentJWSPayloadDigest hashes the exact decoded payload bytes rather than a
// re-marshalled JSON value.
func IntentJWSPayloadDigest(intentJWS string) (string, error) {
	parts := strings.Split(intentJWS, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid money movement intent JWS")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode money movement intent payload: %w", err)
	}
	return digestBytes(payload), nil
}

func validateIntentAgainstAuthorization(intent MoneyMovementIntent, auth SigningAuthorization, policy ApprovalPolicyBundle, policyDigest string, now time.Time) error {
	if !intent.authorizesSource(auth) ||
		intent.TenantID != auth.TenantID ||
		intent.Purpose != auth.Purpose ||
		intent.NetworkCode != auth.NetworkCode ||
		intent.TokenStandard != auth.AssetStandard ||
		intent.ContractAddress != auth.ContractAddress ||
		intent.DestinationAddress != auth.DestinationAddress ||
		intent.PolicyVersion != policy.PolicyVersion ||
		intent.PolicyDigest != policyDigest {
		return fmt.Errorf("money movement intent does not match authorization")
	}
	if auth.Purpose == "withdrawal" {
		if auth.MerchantID == nil || intent.MerchantID == nil || *auth.MerchantID != *intent.MerchantID {
			return fmt.Errorf("withdrawal intent merchant_id does not match authorization")
		}
	}
	if auth.Purpose == "treasury_withdrawal" {
		if intent.SourceAddress != auth.SourceAddress || intent.SourceVaultKeyID != auth.VaultKeyID {
			return fmt.Errorf("treasury intent source wallet does not match authorization")
		}
	}
	if intent.ExpiresAt.IsZero() || !now.Before(intent.ExpiresAt) {
		return fmt.Errorf("money movement intent expired")
	}
	return nil
}

func requiredPrincipals(policy ApprovalPolicyBundle, intent MoneyMovementIntent, risk RiskAssessment) ([]ApprovalPrincipal, []string, error) {
	var required []ApprovalPrincipal
	matched := make([]string, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		if !ruleMatches(rule, intent, risk) {
			continue
		}
		ruleRequired, err := normalizePrincipals(rule.RequiredPrincipals)
		if err != nil {
			return nil, nil, err
		}
		required = unionPrincipals(required, ruleRequired)
		matched = append(matched, rule.ID)
	}
	if len(matched) == 0 {
		var err error
		required, err = normalizePrincipals(policy.DefaultRequiredPrincipals)
		if err != nil {
			return nil, nil, err
		}
	}
	return required, matched, nil
}

func ruleMatches(rule ApprovalPolicyRule, intent MoneyMovementIntent, risk RiskAssessment) bool {
	if rule.Purpose != "" && rule.Purpose != intent.Purpose {
		return false
	}
	if rule.NetworkCode != "" && rule.NetworkCode != intent.NetworkCode {
		return false
	}
	if rule.TokenID != "" && rule.TokenID != intent.TokenID {
		return false
	}
	if rule.AmountMinorLTE != nil && intent.AmountMinorUnits.Cmp(*rule.AmountMinorLTE) > 0 {
		return false
	}
	if rule.AmountMinorGTE != nil && intent.AmountMinorUnits.Cmp(*rule.AmountMinorGTE) < 0 {
		return false
	}
	if len(rule.RiskLevelIn) > 0 && !slices.Contains(rule.RiskLevelIn, risk.RiskLevel) {
		return false
	}
	return true
}

func normalizePrincipals(in []ApprovalPrincipal) ([]ApprovalPrincipal, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("required principals cannot be empty")
	}
	seen := map[ApprovalPrincipal]bool{}
	out := make([]ApprovalPrincipal, 0, len(in))
	for _, principal := range in {
		if principal != ApprovalPrincipalAdmin && principal != ApprovalPrincipalRisk {
			return nil, fmt.Errorf("unsupported principal: %s", principal)
		}
		if seen[principal] {
			continue
		}
		seen[principal] = true
		out = append(out, principal)
	}
	slices.Sort(out)
	return out, nil
}

func unionPrincipals(left, right []ApprovalPrincipal) []ApprovalPrincipal {
	seen := map[ApprovalPrincipal]bool{}
	out := make([]ApprovalPrincipal, 0, len(left)+len(right))
	for _, principal := range append(left, right...) {
		if seen[principal] {
			continue
		}
		seen[principal] = true
		out = append(out, principal)
	}
	slices.Sort(out)
	return out
}

func digestJSON(v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return digestBytes(payload), nil
}

func digestBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
