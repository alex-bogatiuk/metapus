package vault_signer

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/vaultclient"
)

type Config struct {
	TenantID string
	Client   *vaultclient.Client
}

// VaultSigner implements tenant-scoped, policy-aware TRON signing. The shared
// client is tokenless behind Vault Proxy in production.
type VaultSigner struct {
	client   *vaultclient.Client
	tenantID string
}

func New(cfg Config) (*VaultSigner, error) {
	tenantID := strings.ToLower(strings.TrimSpace(cfg.TenantID))
	parsed, err := id.Parse(tenantID)
	if err != nil || parsed.String() != tenantID {
		return nil, fmt.Errorf("vault signer tenant_id must be a canonical UUID")
	}
	if cfg.Client == nil {
		return nil, fmt.Errorf("shared Vault client is required")
	}
	return &VaultSigner{client: cfg.Client, tenantID: tenantID}, nil
}

func (s *VaultSigner) deriveTRONAddress(ctx context.Context, networkIdentifier string, derivationPath string) (crypto.WalletDerivation, error) {
	pluginNetwork, err := tronPluginNetwork(networkIdentifier)
	if err != nil {
		return crypto.WalletDerivation{}, err
	}
	request := map[string]string{"derivation_path": derivationPath, "network": pluginNetwork}
	var response struct {
		Data struct {
			Address        string `json:"address"`
			VaultKeyID     string `json:"vault_key_id"`
			DerivationPath string `json:"derivation_path"`
		} `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodPost, s.endpoint("wallets/derive"), request, &response); err != nil {
		return crypto.WalletDerivation{}, fmt.Errorf("vault generate address: %w", err)
	}
	if response.Data.Address == "" || response.Data.VaultKeyID == "" || response.Data.DerivationPath == "" {
		return crypto.WalletDerivation{}, fmt.Errorf("vault generate address returned incomplete derivation")
	}
	return crypto.WalletDerivation{
		Address: response.Data.Address, VaultKeyID: response.Data.VaultKeyID, DerivationPath: response.Data.DerivationPath,
	}, nil
}

func (s *VaultSigner) DeriveAddress(ctx context.Context, network crypto.NetworkRef, derivationPath string) (crypto.WalletDerivation, error) {
	if network.Family != crypto.ChainFamilyTRON {
		return crypto.WalletDerivation{}, fmt.Errorf("TRON signer cannot derive family %s", network.Family)
	}
	derived, err := s.deriveTRONAddress(ctx, network.NetworkIdentifier, derivationPath)
	if err != nil {
		return crypto.WalletDerivation{}, err
	}
	return crypto.WalletDerivation{Address: derived.Address, VaultKeyID: derived.VaultKeyID, DerivationPath: derived.DerivationPath}, nil
}

func (s *VaultSigner) Family() crypto.ChainFamily { return crypto.ChainFamilyTRON }

func (s *VaultSigner) CheckReady(ctx context.Context, network crypto.NetworkRef, authorizationKID string) error {
	if network.Family != crypto.ChainFamilyTRON {
		return fmt.Errorf("TRON signer cannot serve family %s", network.Family)
	}
	return s.checkTRONSignerReady(ctx, network.NetworkIdentifier, authorizationKID)
}

func (s *VaultSigner) Sign(ctx context.Context, request crypto.PolicySigningRequest) (crypto.SignedTxEnvelope, error) {
	unsigned := request.UnsignedEnvelope
	if unsigned.Family != crypto.ChainFamilyTRON || unsigned.UnsignedFingerprint == "" || unsigned.Generation != 1 || unsigned.NetworkSequence != "" {
		return crypto.SignedTxEnvelope{}, fmt.Errorf("invalid TRON unsigned envelope")
	}
	var response struct {
		Data struct {
			SignedEnvelope crypto.SignedTxEnvelope `json:"signed_envelope"`
		} `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodPost, s.endpoint("tx/sign"), request, &response); err != nil {
		return crypto.SignedTxEnvelope{}, fmt.Errorf("vault sign TRON envelope: %w", err)
	}
	signed := response.Data.SignedEnvelope
	if signed.Family != crypto.ChainFamilyTRON || signed.SchemaVersion != unsigned.SchemaVersion || signed.TxHash == "" ||
		signed.UnsignedFingerprint != unsigned.UnsignedFingerprint || signed.Generation != unsigned.Generation ||
		signed.NetworkSequence != unsigned.NetworkSequence || len(signed.Payload) == 0 {
		return crypto.SignedTxEnvelope{}, fmt.Errorf("vault sign TRON envelope returned inconsistent result")
	}
	return signed, nil
}

func (s *VaultSigner) checkTRONSignerReady(ctx context.Context, networkIdentifier string, authorizationKID string) error {
	var response struct {
		Data struct {
			SchemaVersion                string   `json:"schema_version"`
			AuthorizationKeyringReady    bool     `json:"authorization_keyring_configured"`
			AuthorizationKeyIDs          []string `json:"authorization_key_ids"`
			ApprovalPublicKeysConfigured bool     `json:"approval_public_keys_configured"`
			HDSeedConfigured             bool     `json:"hd_seed_configured"`
			SweepPolicyNetworks          []string `json:"sweep_policy_networks"`
		} `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodGet, s.endpoint("config/readiness"), nil, &response); err != nil {
		return fmt.Errorf("vault signer readiness: %w", err)
	}
	if response.Data.SchemaVersion != "tron_tenant_signing_config.v3" ||
		!response.Data.AuthorizationKeyringReady ||
		!response.Data.ApprovalPublicKeysConfigured ||
		!response.Data.HDSeedConfigured {
		return fmt.Errorf("vault signer readiness: tenant signing configuration is incomplete")
	}
	if !slices.Contains(response.Data.AuthorizationKeyIDs, authorizationKID) {
		return fmt.Errorf("vault signer readiness: authorization kid is not in tenant keyring")
	}
	pluginNetwork, err := tronPluginNetwork(networkIdentifier)
	if err != nil {
		return err
	}
	if !slices.Contains(response.Data.SweepPolicyNetworks, pluginNetwork) {
		return fmt.Errorf("vault signer readiness: sweep policy is not configured for network")
	}
	return nil
}

func tronPluginNetwork(networkIdentifier string) (string, error) {
	identifier := strings.ToLower(strings.TrimSpace(networkIdentifier))
	if identifier == "" {
		return "", fmt.Errorf("TRON network identifier is required")
	}
	return "tron_" + identifier, nil
}

func (s *VaultSigner) endpoint(path string) string {
	return "/v1/tron/tenants/" + url.PathEscape(s.tenantID) + "/" + path
}

var _ crypto.PolicyAwareTransactionSigner = (*VaultSigner)(nil)
var _ crypto.WalletDeriver = (*VaultSigner)(nil)

func (s *VaultSigner) CheckMerchantCustodyReady(ctx context.Context, network crypto.NetworkRef) error {
	if network.Family != crypto.ChainFamilyTRON {
		return fmt.Errorf("incorrect custody signer family")
	}
	pluginNetwork, err := tronPluginNetwork(network.NetworkIdentifier)
	if err != nil {
		return err
	}
	var response struct {
		Data struct {
			Supported bool     `json:"merchant_custody_supported"`
			Schema    string   `json:"merchant_custody_schema"`
			Revision  int      `json:"merchant_custody_revision"`
			Networks  []string `json:"merchant_custody_networks"`
		} `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodGet, s.endpoint("config/readiness"), nil, &response); err != nil {
		return fmt.Errorf("vault merchant custody readiness: %w", err)
	}
	if !response.Data.Supported || response.Data.Schema != "merchant_custody.v1" || response.Data.Revision <= 0 || !slices.Contains(response.Data.Networks, pluginNetwork) {
		return fmt.Errorf("vault merchant custody enforcement is not configured for network")
	}
	return nil
}

// CheckAMLReady reads binary capabilities rather than a mutable enable flag.
func (s *VaultSigner) CheckAMLReady(ctx context.Context, network crypto.NetworkRef) error {
	if network.Family != crypto.ChainFamilyTRON {
		return fmt.Errorf("incorrect AML signer family")
	}
	var response struct {
		Data crypto.AMLSigningContract `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodGet, s.endpoint("config/readiness"), nil, &response); err != nil {
		return fmt.Errorf("vault AML signing readiness: %w", err)
	}
	return response.Data.Validate()
}

var _ crypto.AMLSigner = (*VaultSigner)(nil)
