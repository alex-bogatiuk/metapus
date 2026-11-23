package crypto_worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto/custody"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

// AMLConfigurationPreflight owns readiness ports only. It cannot start worker
// loops and does not construct approval verifiers, RPC drivers or posting services.
type AMLConfigurationPreflight struct {
	tenantID       string
	signers        *crypto.FamilySignerRegistry
	authorizations signingAuthorizationReadiness
}

func NewAMLConfigurationPreflight(cfg CryptoProcessorConfig) (*AMLConfigurationPreflight, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "production") {
		if strings.TrimSpace(cfg.VaultToken) != "" {
			return nil, fmt.Errorf("VAULT_TOKEN is forbidden in production; use tokenless Vault Proxy mode")
		}
		if strings.TrimSpace(cfg.SigningAuthorizationPrivateKey) != "" {
			return nil, fmt.Errorf("CRYPTO_SIGNING_AUTH_PRIVATE_KEY is forbidden in production; use Vault Transit")
		}
	}
	var client *vaultclient.Client
	var err error
	if strings.TrimSpace(cfg.VaultAddr) != "" {
		client, err = vaultclient.New(vaultclient.Config{Addr: cfg.VaultAddr, Token: cfg.VaultToken})
		if err != nil {
			return nil, fmt.Errorf("configure Vault readiness client: %w", err)
		}
	}
	signers, _, err := custody.Build(custody.Config{Client: client, TenantID: cfg.TenantID})
	if err != nil {
		return nil, err
	}
	control, err := configuredAuthorizationSigner(cfg, client)
	if err != nil {
		return nil, err
	}
	result := &AMLConfigurationPreflight{tenantID: cfg.TenantID, signers: signers}
	if control != nil {
		result.authorizations, err = NewSigningAuthorizationService(NewOutboundRepo(), control, cfg.TenantID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (p *CryptoProcessor) RunAMLConfigurationPreflight(ctx context.Context, networkCode string) (crypto.AMLConfigurationReport, error) {
	preflight := AMLConfigurationPreflight{tenantID: p.cfg.TenantID, signers: p.familySigners}
	if p.authorizations != nil {
		preflight.authorizations = p.authorizations
	}
	return preflight.Run(ctx, networkCode)
}

// RunAMLConfigurationPreflight uses SQL SELECTs and signer readiness GETs only.
// Network dispatch may remain disabled while preparing an AML configuration.
func (p *AMLConfigurationPreflight) Run(ctx context.Context, networkCode string) (crypto.AMLConfigurationReport, error) {
	namespace, parseErr := id.Parse(p.tenantID)
	result, err := crypto_repo.NewAMLRepository().ReadAMLConfiguration(ctx, strings.TrimSpace(networkCode), namespace)
	if err != nil {
		return result, err
	}
	if parseErr != nil || id.IsNil(namespace) {
		result.Blockers = append(result.Blockers, "tenant_namespace_invalid")
	}
	if len(result.Blockers) > 0 {
		return result, nil
	}
	for i := range result.Networks {
		network := &result.Networks[i]
		// Keep the reason stable and secret-free: transport errors can contain
		// internal Vault endpoints. Operator logs/readiness GETs provide detail.
		checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if blocker := p.checkAMLConfigurationSigner(checkCtx, network.Network); blocker != "" {
			network.Blockers = append(network.Blockers, blocker)
		} else {
			network.SignerReady = true
		}
		cancel()
	}
	result.Ready = len(result.Networks) > 0
	for _, network := range result.Networks {
		result.Ready = result.Ready && len(network.Blockers) == 0 && network.SignerReady
	}
	return result, nil
}

func (p *AMLConfigurationPreflight) checkAMLConfigurationSigner(ctx context.Context, network crypto.NetworkRef) string {
	if p.signers == nil || p.authorizations == nil {
		return "signer_not_configured"
	}
	signer, err := p.signers.Resolve(network.Family)
	if err != nil {
		return "family_signer_unavailable"
	}
	kid, err := p.authorizations.CheckReady(ctx)
	if err != nil {
		return "authorization_signer_not_ready"
	}
	if err = signer.CheckReady(ctx, network, kid); err != nil {
		return "family_signer_not_ready"
	}
	custody, ok := signer.(crypto.MerchantCustodySigner)
	if !ok {
		return "custody_signer_unavailable"
	}
	if err = custody.CheckMerchantCustodyReady(ctx, network); err != nil {
		return "custody_signer_not_ready"
	}
	amlSigner, ok := signer.(crypto.AMLSigner)
	if !ok {
		return "aml_signer_contract_unavailable"
	}
	if err = amlSigner.CheckAMLReady(ctx, network); err != nil {
		return "aml_signer_contract_incompatible"
	}
	return ""
}
