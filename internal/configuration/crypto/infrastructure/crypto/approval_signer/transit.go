package approval_signer

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

var vaultMountPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var approvalRoles = []crypto.ApprovalSigningRole{
	crypto.ApprovalSigningRoleIntent,
	crypto.ApprovalSigningRoleRiskAssessment,
	crypto.ApprovalSigningRolePolicyDecision,
	crypto.ApprovalSigningRoleAdminApproval,
	crypto.ApprovalSigningRoleRiskApproval,
}

type TransitProviderConfig struct {
	Client          *vaultclient.Client
	Mount           string
	PolicyPublicKey ed25519.PublicKey
	PolicyJWS       map[string]string
}

// TransitProvider owns no private key material. It resolves immutable public
// key/version snapshots at startup and delegates every signature to Vault
// Transit using a distinct non-exportable key per tenant and approval role.
type TransitProvider struct {
	client     *vaultclient.Client
	mount      string
	policies   map[string]crypto.SignedApprovalPolicy
	publicKeys map[string]crypto.ApprovalPublicKeys
	keyRefs    map[string]map[crypto.ApprovalSigningRole]transitKeyRef
	now        func() time.Time
}

type transitKeyRef struct {
	name      string
	version   int
	kid       string
	publicKey ed25519.PublicKey
}

func NewTransitProvider(ctx context.Context, cfg TransitProviderConfig) (*TransitProvider, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("vault client is required")
	}
	mount := strings.TrimSpace(cfg.Mount)
	if mount == "" {
		mount = "transit"
	}
	if !vaultMountPattern.MatchString(mount) {
		return nil, fmt.Errorf("invalid Transit mount")
	}
	if len(cfg.PolicyJWS) == 0 {
		return nil, fmt.Errorf("at least one signed approval policy bundle is required")
	}

	p := &TransitProvider{
		client:     cfg.Client,
		mount:      mount,
		policies:   make(map[string]crypto.SignedApprovalPolicy, len(cfg.PolicyJWS)),
		publicKeys: make(map[string]crypto.ApprovalPublicKeys, len(cfg.PolicyJWS)),
		keyRefs:    make(map[string]map[crypto.ApprovalSigningRole]transitKeyRef, len(cfg.PolicyJWS)),
		now:        func() time.Time { return time.Now().UTC() },
	}
	now := p.now()
	for label, rawPolicy := range cfg.PolicyJWS {
		policy, err := crypto.NewSignedApprovalPolicy(rawPolicy, cfg.PolicyPublicKey, now)
		if err != nil {
			return nil, fmt.Errorf("invalid signed approval policy %q: %w", label, err)
		}
		tenantID := policy.Policy.TenantID
		if _, exists := p.policies[tenantID]; exists {
			return nil, fmt.Errorf("duplicate signed approval policy for tenant %s", tenantID)
		}
		refs, keys, err := p.loadTenantKeys(ctx, tenantID, cfg.PolicyPublicKey)
		if err != nil {
			return nil, fmt.Errorf("load approval signing keys for tenant %s: %w", tenantID, err)
		}
		p.policies[tenantID] = policy
		p.keyRefs[tenantID] = refs
		p.publicKeys[tenantID] = keys
	}
	return p, nil
}

func (p *TransitProvider) SignerForTenant(tenantID string) (*crypto.MoneyMovementApprovalSigner, error) {
	policy, ok := p.policies[tenantID]
	if !ok {
		return nil, fmt.Errorf("signed approval policy is not configured for tenant %s", tenantID)
	}
	if err := policy.Policy.Validate(p.now()); err != nil {
		return nil, err
	}
	return crypto.NewMoneyMovementApprovalSigner(p, p.publicKeys[tenantID], policy)
}

func (p *TransitProvider) PublicKeysForTenant(tenantID string) (crypto.ApprovalPublicKeys, error) {
	keys, ok := p.publicKeys[tenantID]
	if !ok {
		return crypto.ApprovalPublicKeys{}, fmt.Errorf("approval keys are not configured for tenant %s", tenantID)
	}
	return keys, nil
}

func (p *TransitProvider) SignApprovalJWS(ctx context.Context, tenantID string, role crypto.ApprovalSigningRole, payload any) (string, error) {
	refs, ok := p.keyRefs[tenantID]
	if !ok {
		return "", fmt.Errorf("approval keys are not configured for tenant %s", tenantID)
	}
	ref, ok := refs[role]
	if !ok {
		return "", fmt.Errorf("unsupported approval signing role %q", role)
	}
	input, err := crypto.BuildApprovalJWSSigningInput(payload, ref.kid)
	if err != nil {
		return "", err
	}
	request := map[string]any{
		"input":       base64.StdEncoding.EncodeToString([]byte(input)),
		"key_version": ref.version,
	}
	var response struct {
		Data struct {
			Signature string `json:"signature"`
		} `json:"data"`
	}
	if err := p.client.DoJSON(ctx, http.MethodPost, "/v1/"+p.mount+"/sign/"+ref.name, request, &response); err != nil {
		return "", fmt.Errorf("transit sign %s approval artifact: %w", role, err)
	}
	version, signature, err := parseTransitSignature(response.Data.Signature)
	if err != nil {
		return "", err
	}
	if version != ref.version {
		return "", fmt.Errorf("transit returned key version %d, requested %d", version, ref.version)
	}
	return crypto.CompleteApprovalJWS(input, signature)
}

func (p *TransitProvider) loadTenantKeys(ctx context.Context, tenantID string, policyPublicKey ed25519.PublicKey) (map[crypto.ApprovalSigningRole]transitKeyRef, crypto.ApprovalPublicKeys, error) {
	refs := make(map[crypto.ApprovalSigningRole]transitKeyRef, len(approvalRoles))
	for _, role := range approvalRoles {
		name, err := transitApprovalKeyName(tenantID, role)
		if err != nil {
			return nil, crypto.ApprovalPublicKeys{}, err
		}
		ref, err := p.readKeyRef(ctx, name)
		if err != nil {
			return nil, crypto.ApprovalPublicKeys{}, fmt.Errorf("%s: %w", role, err)
		}
		refs[role] = ref
	}
	keys := crypto.ApprovalPublicKeys{
		PolicyPublicKey:        policyPublicKey,
		IntentPublicKey:        refs[crypto.ApprovalSigningRoleIntent].publicKey,
		RiskPublicKey:          refs[crypto.ApprovalSigningRoleRiskAssessment].publicKey,
		DecisionPublicKey:      refs[crypto.ApprovalSigningRolePolicyDecision].publicKey,
		AdminApprovalPublicKey: refs[crypto.ApprovalSigningRoleAdminApproval].publicKey,
		RiskApprovalPublicKey:  refs[crypto.ApprovalSigningRoleRiskApproval].publicKey,
	}
	if err := crypto.ValidateApprovalPublicKeys(keys); err != nil {
		return nil, crypto.ApprovalPublicKeys{}, err
	}
	return refs, keys, nil
}

type transitKeyMetadata struct {
	Data struct {
		Type            string `json:"type"`
		Exportable      bool   `json:"exportable"`
		DeletionAllowed bool   `json:"deletion_allowed"`
		LatestVersion   int    `json:"latest_version"`
		Keys            map[string]struct {
			PublicKey string `json:"public_key"`
		} `json:"keys"`
	} `json:"data"`
}

func (p *TransitProvider) readKeyRef(ctx context.Context, name string) (transitKeyRef, error) {
	var metadata transitKeyMetadata
	if err := p.client.DoJSON(ctx, http.MethodGet, "/v1/"+p.mount+"/keys/"+name, nil, &metadata); err != nil {
		return transitKeyRef{}, fmt.Errorf("read Transit key metadata: %w", err)
	}
	if metadata.Data.Type != "ed25519" || metadata.Data.Exportable || metadata.Data.DeletionAllowed {
		return transitKeyRef{}, fmt.Errorf("transit key must be non-exportable, non-deletable ed25519")
	}
	version := metadata.Data.LatestVersion
	keyVersion, ok := metadata.Data.Keys[strconv.Itoa(version)]
	if version < 1 || !ok || strings.TrimSpace(keyVersion.PublicKey) == "" {
		return transitKeyRef{}, fmt.Errorf("transit key has no active public key")
	}
	publicKey, err := crypto.ParseEd25519PublicKey(keyVersion.PublicKey)
	if err != nil {
		return transitKeyRef{}, fmt.Errorf("parse Transit public key: %w", err)
	}
	return transitKeyRef{
		name:      name,
		version:   version,
		kid:       p.mount + "/" + name + "/v" + strconv.Itoa(version),
		publicKey: publicKey,
	}, nil
}

func transitApprovalKeyName(tenantID string, role crypto.ApprovalSigningRole) (string, error) {
	parsed, err := id.Parse(strings.TrimSpace(tenantID))
	if err != nil || parsed.String() != strings.ToLower(strings.TrimSpace(tenantID)) {
		return "", fmt.Errorf("tenant_id must be a canonical UUID")
	}
	if slices.Contains(approvalRoles, role) {
		return "metapus-approval-" + string(role) + "-" + parsed.String(), nil
	}
	return "", fmt.Errorf("unsupported approval signing role %q", role)
}

func parseTransitSignature(value string) (int, []byte, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != "vault" || !strings.HasPrefix(parts[1], "v") {
		return 0, nil, fmt.Errorf("invalid Transit signature envelope")
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[1], "v"))
	if err != nil || version < 1 {
		return 0, nil, fmt.Errorf("invalid Transit signature key version")
	}
	var signature []byte
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		signature, err = encoding.DecodeString(parts[2])
		if err == nil {
			break
		}
	}
	if err != nil || len(signature) != ed25519.SignatureSize {
		return 0, nil, fmt.Errorf("invalid Transit Ed25519 signature")
	}
	return version, signature, nil
}

var _ crypto.MoneyMovementApprovalSignerProvider = (*TransitProvider)(nil)
var _ crypto.ApprovalJWSSigner = (*TransitProvider)(nil)
