package evm_vault_signer

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/internal/core/id"
)

type Config struct {
	TenantID string
	Client   *vaultclient.Client
}

type VaultSigner struct {
	client   *vaultclient.Client
	tenantID string
}

func New(cfg Config) (*VaultSigner, error) {
	tenantID := strings.ToLower(strings.TrimSpace(cfg.TenantID))
	parsed, err := id.Parse(tenantID)
	if err != nil || parsed.String() != tenantID || cfg.Client == nil {
		return nil, fmt.Errorf("EVM Vault signer requires canonical tenant UUID and shared Vault client")
	}
	return &VaultSigner{client: cfg.Client, tenantID: tenantID}, nil
}

func (s *VaultSigner) Family() crypto.ChainFamily { return crypto.ChainFamilyEVM }

func (s *VaultSigner) DeriveAddress(ctx context.Context, network crypto.NetworkRef, derivationPath string) (crypto.WalletDerivation, error) {
	if network.Family != crypto.ChainFamilyEVM || strings.TrimSpace(network.NetworkIdentifier) == "" {
		return crypto.WalletDerivation{}, fmt.Errorf("EVM signer network identity is invalid")
	}
	request := map[string]string{"derivation_path": derivationPath, "chain_id": network.NetworkIdentifier}
	var response struct {
		Data struct {
			Address        string `json:"address"`
			VaultKeyID     string `json:"vault_key_id"`
			DerivationPath string `json:"derivation_path"`
		} `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodPost, s.endpoint("wallets/derive"), request, &response); err != nil {
		return crypto.WalletDerivation{}, fmt.Errorf("vault derive EVM address: %w", err)
	}
	if response.Data.Address == "" || response.Data.VaultKeyID == "" || response.Data.DerivationPath != derivationPath {
		return crypto.WalletDerivation{}, fmt.Errorf("vault returned incomplete EVM derivation")
	}
	return crypto.WalletDerivation{Address: response.Data.Address, VaultKeyID: response.Data.VaultKeyID, DerivationPath: response.Data.DerivationPath}, nil
}

func (s *VaultSigner) CheckReady(ctx context.Context, network crypto.NetworkRef, authorizationKID string) error {
	if network.Family != crypto.ChainFamilyEVM {
		return fmt.Errorf("EVM signer cannot serve family %s", network.Family)
	}
	var response struct {
		Data struct {
			SchemaVersion             string   `json:"schema_version"`
			AuthorizationKeyringReady bool     `json:"authorization_keyring_configured"`
			AuthorizationKeyIDs       []string `json:"authorization_key_ids"`
			ApprovalKeysReady         bool     `json:"approval_public_keys_configured"`
			HDSeedReady               bool     `json:"hd_seed_configured"`
			AllowedChainIDs           []string `json:"allowed_chain_ids"`
			SweepPolicyChainIDs       []string `json:"sweep_policy_chain_ids"`
		} `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodGet, s.endpoint("config/readiness"), nil, &response); err != nil {
		return fmt.Errorf("vault EVM signer readiness: %w", err)
	}
	if response.Data.SchemaVersion != "evm_tenant_signing_config.v1" || !response.Data.AuthorizationKeyringReady ||
		!response.Data.ApprovalKeysReady || !response.Data.HDSeedReady ||
		!slices.Contains(response.Data.AuthorizationKeyIDs, authorizationKID) ||
		!slices.Contains(response.Data.AllowedChainIDs, network.NetworkIdentifier) ||
		!slices.Contains(response.Data.SweepPolicyChainIDs, network.NetworkIdentifier) {
		return fmt.Errorf("vault EVM signer readiness configuration is incomplete")
	}
	return nil
}

func (s *VaultSigner) Sign(ctx context.Context, request crypto.PolicySigningRequest) (crypto.SignedTxEnvelope, error) {
	unsigned := request.UnsignedEnvelope
	if unsigned.Family != crypto.ChainFamilyEVM || unsigned.UnsignedFingerprint == "" || unsigned.Generation <= 0 || unsigned.NetworkSequence == "" {
		return crypto.SignedTxEnvelope{}, fmt.Errorf("invalid EVM unsigned envelope")
	}
	var response struct {
		Data struct {
			SignedEnvelope crypto.SignedTxEnvelope `json:"signed_envelope"`
		} `json:"data"`
	}
	if err := s.client.DoJSON(ctx, http.MethodPost, s.endpoint("tx/sign"), request, &response); err != nil {
		return crypto.SignedTxEnvelope{}, fmt.Errorf("vault sign EVM envelope: %w", err)
	}
	signed := response.Data.SignedEnvelope
	if signed.Family != crypto.ChainFamilyEVM || signed.TxHash == "" || signed.UnsignedFingerprint != unsigned.UnsignedFingerprint ||
		signed.Generation != unsigned.Generation || signed.NetworkSequence != unsigned.NetworkSequence || len(signed.Payload) == 0 {
		return crypto.SignedTxEnvelope{}, fmt.Errorf("vault returned inconsistent EVM signed envelope")
	}
	return signed, nil
}

func (s *VaultSigner) endpoint(path string) string {
	return "/v1/evm/tenants/" + url.PathEscape(s.tenantID) + "/" + path
}

var (
	_ crypto.PolicyAwareTransactionSigner = (*VaultSigner)(nil)
	_ crypto.WalletDeriver                = (*VaultSigner)(nil)
)

func (s *VaultSigner) CheckMerchantCustodyReady(ctx context.Context, network crypto.NetworkRef) error {
	if network.Family != crypto.ChainFamilyEVM {
		return fmt.Errorf("incorrect custody signer family")
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
	if !response.Data.Supported || response.Data.Schema != "merchant_custody.v1" || response.Data.Revision <= 0 || !slices.Contains(response.Data.Networks, network.NetworkIdentifier) {
		return fmt.Errorf("vault merchant custody enforcement is not configured for network")
	}
	return nil
}

// CheckAMLReady reads binary capabilities rather than a mutable enable flag.
func (s *VaultSigner) CheckAMLReady(ctx context.Context, network crypto.NetworkRef) error {
	if network.Family != crypto.ChainFamilyEVM {
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
