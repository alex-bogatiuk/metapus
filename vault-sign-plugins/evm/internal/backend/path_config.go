package backend

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"vault-plugin-secrets-evm/internal/evm"
	"vault-plugin-secrets-evm/internal/json"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

const tenantSigningConfigSchemaV1 = "evm_tenant_signing_config.v1"

var (
	tenantIDPattern         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	authorizationKIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+/metapus-signing-auth-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/v[1-9][0-9]*$`)
)

type authorizationConfig struct {
	SchemaVersion           string            `json:"schema_version"`
	AuthorizationKeyring    map[string]string `json:"authorization_keyring"`
	PolicyPublicKey         string            `json:"policy_public_key"`
	IntentPublicKey         string            `json:"intent_public_key"`
	RiskAssessmentPublicKey string            `json:"risk_assessment_public_key"`
	PolicyDecisionPublicKey string            `json:"policy_decision_public_key"`
	AdminApprovalPublicKey  string            `json:"admin_approval_public_key"`
	RiskApprovalPublicKey   string            `json:"risk_approval_public_key"`
	HDSeed                  string            `json:"hd_seed"`
	AllowedChainIDs         []string          `json:"allowed_chain_ids"`
}

func pathConfig(b *evmBackend) []*framework.Path {
	commonFields := map[string]*framework.FieldSchema{
		"tenant_id": {Type: framework.TypeString, Description: "Tenant scope from the Vault path"},
	}
	return []*framework.Path{
		{
			Pattern: "tenants/" + framework.GenericNameRegex("tenant_id") + "/config/authorization",
			Fields: map[string]*framework.FieldSchema{
				"tenant_id":                  {Type: framework.TypeString, Description: "Tenant scope from the Vault path"},
				"schema_version":             {Type: framework.TypeString, Required: true, Description: "Must be evm_tenant_signing_config.v1"},
				"authorization_keyring":      {Type: framework.TypeMap, Required: true, Description: "Transit kid to Ed25519 public key map"},
				"policy_public_key":          {Type: framework.TypeString, Required: true},
				"intent_public_key":          {Type: framework.TypeString, Required: true},
				"risk_assessment_public_key": {Type: framework.TypeString, Required: true},
				"policy_decision_public_key": {Type: framework.TypeString, Required: true},
				"admin_approval_public_key":  {Type: framework.TypeString, Required: true},
				"risk_approval_public_key":   {Type: framework.TypeString, Required: true},
				"hd_seed":                    {Type: framework.TypeString, Required: true, Description: "Encoded 64-byte BIP-39 seed; stored seal-wrapped"},
				"allowed_chain_ids":          {Type: framework.TypeStringSlice, Required: true, Description: "EVM chain ID allowlist; v1 accepts only Sepolia 11155111"},
			},
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.UpdateOperation: &framework.PathOperation{Callback: b.pathConfigAuthorizationWrite},
				logical.ReadOperation:   &framework.PathOperation{Callback: b.pathConfigAuthorizationRead},
			},
			HelpSynopsis: "Configures the tenant authorization keyring, approval verification keys, and HD seed",
		},
		{
			Pattern: "tenants/" + framework.GenericNameRegex("tenant_id") + "/config/readiness",
			Fields:  commonFields,
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.ReadOperation: &framework.PathOperation{Callback: b.pathConfigReadiness},
			},
			HelpSynopsis: "Returns non-secret tenant signing readiness state",
		},
	}
}

func (b *evmBackend) pathConfigAuthorizationWrite(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := strings.TrimSpace(data.Get("tenant_id").(string))
	if !tenantIDPattern.MatchString(tenantID) {
		return logical.ErrorResponse("tenant_id must be a canonical UUID"), nil
	}
	if data.Get("schema_version").(string) != tenantSigningConfigSchemaV1 {
		return logical.ErrorResponse("schema_version must be %s", tenantSigningConfigSchemaV1), nil
	}
	keyring, err := decodeStringMap(data.Get("authorization_keyring"))
	if err != nil || len(keyring) == 0 {
		return logical.ErrorResponse("invalid authorization_keyring: a non-empty string map is required"), nil
	}
	cfg := authorizationConfig{
		SchemaVersion: tenantSigningConfigSchemaV1, AuthorizationKeyring: keyring,
		PolicyPublicKey: data.Get("policy_public_key").(string), IntentPublicKey: data.Get("intent_public_key").(string),
		RiskAssessmentPublicKey: data.Get("risk_assessment_public_key").(string),
		PolicyDecisionPublicKey: data.Get("policy_decision_public_key").(string),
		AdminApprovalPublicKey:  data.Get("admin_approval_public_key").(string),
		RiskApprovalPublicKey:   data.Get("risk_approval_public_key").(string), HDSeed: data.Get("hd_seed").(string),
		AllowedChainIDs: data.Get("allowed_chain_ids").([]string),
	}
	if len(cfg.AllowedChainIDs) != 1 || cfg.AllowedChainIDs[0] != evm.SepoliaChainID {
		return logical.ErrorResponse("allowed_chain_ids must contain only Sepolia chain ID %s", evm.SepoliaChainID), nil
	}
	authKeys, err := cfg.authorizationPublicKeys(tenantID)
	if err != nil {
		return logical.ErrorResponse("invalid authorization_keyring: %v", err), nil
	}
	approvalKeys, err := cfg.approvalPublicKeys()
	if err != nil {
		return logical.ErrorResponse("invalid approval public keys: %v", err), nil
	}
	if err := validateTenantConfigPublicKeys(authKeys, approvalKeys); err != nil {
		return logical.ErrorResponse("invalid tenant signing key separation: %v", err), nil
	}
	seed, err := parseEncodedSeed(cfg.HDSeed)
	if err != nil || len(seed) != 64 {
		return logical.ErrorResponse("invalid hd_seed: expected encoded 64-byte BIP-39 seed"), nil
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if err := req.Storage.Put(ctx, &logical.StorageEntry{Key: authorizationConfigKey(tenantID), Value: payload, SealWrap: true}); err != nil {
		b.Logger().Error("failed to save authorization config", "error", err, "tenant_id", tenantID)
		return logical.ErrorResponse("failed to save config"), nil
	}
	return &logical.Response{Data: map[string]any{
		"schema_version": tenantSigningConfigSchemaV1, "configured": true,
		"authorization_keyring_configured": true, "approval_public_keys_configured": true, "hd_seed_configured": true,
	}}, nil
}

func (b *evmBackend) pathConfigAuthorizationRead(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	cfg, err := b.getAuthorizationConfig(ctx, req.Storage, data.Get("tenant_id").(string))
	if err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if cfg.SchemaVersion != tenantSigningConfigSchemaV1 {
		return logical.ErrorResponse("authorization config is not set"), nil
	}
	return &logical.Response{Data: map[string]any{
		"schema_version": cfg.SchemaVersion, "authorization_keyring": cfg.AuthorizationKeyring,
		"policy_public_key": cfg.PolicyPublicKey, "intent_public_key": cfg.IntentPublicKey,
		"risk_assessment_public_key": cfg.RiskAssessmentPublicKey, "policy_decision_public_key": cfg.PolicyDecisionPublicKey,
		"admin_approval_public_key": cfg.AdminApprovalPublicKey, "risk_approval_public_key": cfg.RiskApprovalPublicKey,
		"hd_seed_configured": cfg.HDSeed != "",
		"allowed_chain_ids":  cfg.AllowedChainIDs,
	}}, nil
}

func (b *evmBackend) pathConfigReadiness(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := data.Get("tenant_id").(string)
	cfg, err := b.getAuthorizationConfig(ctx, req.Storage, tenantID)
	if err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if cfg.SchemaVersion != tenantSigningConfigSchemaV1 {
		return logical.ErrorResponse("authorization config is not set"), nil
	}
	keyIDs := make([]string, 0, len(cfg.AuthorizationKeyring))
	for kid := range cfg.AuthorizationKeyring {
		keyIDs = append(keyIDs, kid)
	}
	slices.Sort(keyIDs)
	sweepChainIDs := []string{}
	if sweep, sweepErr := b.getSweepDestinationsConfig(ctx, req.Storage, tenantID); sweepErr == nil && sweep.SchemaVersion == sweepDestinationsSchemaV1 {
		for chainID, destinations := range sweep.Destinations {
			if len(destinations) > 0 {
				sweepChainIDs = append(sweepChainIDs, chainID)
			}
		}
		slices.Sort(sweepChainIDs)
	}
	custody, err := b.getMerchantCustodyConfig(ctx, req.Storage, tenantID)
	if err != nil {
		return logical.ErrorResponse("custody registry unavailable"), nil
	}
	_, authorizationErr := cfg.authorizationPublicKeys(tenantID)
	_, approvalErr := cfg.approvalPublicKeys()
	return &logical.Response{Data: map[string]any{
		"schema_version":                   tenantSigningConfigSchemaV1,
		"authorization_keyring_configured": len(keyIDs) > 0 && authorizationErr == nil,
		"authorization_key_ids":            keyIDs,
		"approval_public_keys_configured":  approvalErr == nil,
		"hd_seed_configured":               cfg.HDSeed != "",
		"merchant_custody_supported":       true,
		"aml_signing_schema":               "aml_signing.v1",
		"aml_intent_schema":                "money_movement_intent.v2",
		"aml_source_line_basis":            "CryptoAMLWithdrawalLine",
		"merchant_custody_schema":          merchantCustodySchemaV1,
		"merchant_custody_revision":        custody.Revision,
		"merchant_custody_networks":        custody.EnforcedNetworks,
		"allowed_chain_ids":                cfg.AllowedChainIDs,
		"sweep_policy_chain_ids":           sweepChainIDs,
	}}, nil
}

func (b *evmBackend) getAuthorizationConfig(ctx context.Context, storage logical.Storage, tenantID string) (authorizationConfig, error) {
	var cfg authorizationConfig
	entry, err := storage.Get(ctx, authorizationConfigKey(tenantID))
	if err != nil || entry == nil {
		return cfg, err
	}
	err = json.Unmarshal(entry.Value, &cfg)
	return cfg, err
}

func (cfg authorizationConfig) seedBytes() ([]byte, error) { return parseEncodedSeed(cfg.HDSeed) }

func (cfg authorizationConfig) authorizationPublicKey(tenantID, kid string) (ed25519.PublicKey, error) {
	matches := authorizationKIDPattern.FindStringSubmatch(kid)
	if len(matches) != 2 {
		return nil, fmt.Errorf("invalid authorization kid")
	}
	if matches[1] != tenantID {
		return nil, fmt.Errorf("authorization kid belongs to a different tenant")
	}
	encoded, ok := cfg.AuthorizationKeyring[kid]
	if !ok {
		return nil, fmt.Errorf("authorization kid is not allowed")
	}
	return parseConfigPublicKey("authorization", encoded)
}

func (cfg authorizationConfig) authorizationPublicKeys(tenantID string) (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey, len(cfg.AuthorizationKeyring))
	for kid := range cfg.AuthorizationKeyring {
		key, err := cfg.authorizationPublicKey(tenantID, kid)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", kid, err)
		}
		keys[kid] = key
	}
	return keys, nil
}

func (cfg authorizationConfig) approvalPublicKeys() (evm.ApprovalPublicKeys, error) {
	values := []struct{ name, encoded string }{
		{"policy", cfg.PolicyPublicKey}, {"intent", cfg.IntentPublicKey}, {"risk_assessment", cfg.RiskAssessmentPublicKey},
		{"policy_decision", cfg.PolicyDecisionPublicKey}, {"admin_approval", cfg.AdminApprovalPublicKey}, {"risk_approval", cfg.RiskApprovalPublicKey},
	}
	parsed := make([]ed25519.PublicKey, len(values))
	for i, value := range values {
		key, err := parseConfigPublicKey(value.name, value.encoded)
		if err != nil {
			return evm.ApprovalPublicKeys{}, err
		}
		parsed[i] = key
	}
	keys := evm.ApprovalPublicKeys{PolicyPublicKey: parsed[0], IntentPublicKey: parsed[1], RiskPublicKey: parsed[2], DecisionPublicKey: parsed[3], AdminApprovalPublicKey: parsed[4], RiskApprovalPublicKey: parsed[5]}
	if err := evm.ValidateApprovalPublicKeys(keys); err != nil {
		return evm.ApprovalPublicKeys{}, err
	}
	return keys, nil
}

func parseConfigPublicKey(role, encoded string) (ed25519.PublicKey, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, fmt.Errorf("%s public key is required", role)
	}
	return evm.ParseEd25519PublicKey(encoded)
}

func validateTenantConfigPublicKeys(authKeys map[string]ed25519.PublicKey, approval evm.ApprovalPublicKeys) error {
	seen := map[string]string{}
	for kid, key := range authKeys {
		fingerprint := string(key)
		if previous, ok := seen[fingerprint]; ok {
			return fmt.Errorf("authorization %s public key duplicates %s public key", kid, previous)
		}
		seen[fingerprint] = "authorization " + kid
	}
	roles := []struct {
		name string
		key  ed25519.PublicKey
	}{
		{"policy", approval.PolicyPublicKey}, {"intent", approval.IntentPublicKey}, {"risk_assessment", approval.RiskPublicKey},
		{"policy_decision", approval.DecisionPublicKey}, {"admin_approval", approval.AdminApprovalPublicKey}, {"risk_approval", approval.RiskApprovalPublicKey},
	}
	for _, role := range roles {
		if previous, ok := seen[string(role.key)]; ok {
			return fmt.Errorf("%s public key duplicates %s public key", role.name, previous)
		}
		seen[string(role.key)] = role.name
	}
	return nil
}

func decodeStringMap(raw any) (map[string]string, error) {
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result map[string]string
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func parseEncodedSeed(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, fmt.Errorf("empty seed")
	}
	decoders := []func(string) ([]byte, error){base64.RawURLEncoding.DecodeString, base64.StdEncoding.DecodeString, base64.RawStdEncoding.DecodeString, decodeHexString}
	for _, decode := range decoders {
		if raw, err := decode(encoded); err == nil {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("unsupported encoding")
}

func decodeHexString(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	return hex.DecodeString(s)
}
