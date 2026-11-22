package content

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/application/checkout"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	domaincrypto "metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	chainbootstrap "metapus/internal/configuration/crypto/infrastructure/blockchain/bootstrap"
	"metapus/internal/configuration/crypto/infrastructure/crypto/approval_signer"
	"metapus/internal/configuration/crypto/infrastructure/crypto/cryptoprofiles"
	"metapus/internal/configuration/crypto/infrastructure/crypto/cryptoregisters"
	"metapus/internal/configuration/crypto/infrastructure/crypto/webhooksecret"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_catalog_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_integration_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/portal_repo"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/pkg/sdk/configuration"
	"metapus/pkg/sdk/envconfig"
	"metapus/pkg/sdk/http/v1"
	"metapus/pkg/sdk/integrations/integrationapp"
	"metapus/pkg/sdk/integrations/transport"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/numerator"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/postgres/integration_repo"
	"metapus/pkg/sdk/posting"
)

// serverSettings are the crypto configuration's API server settings.
type serverSettings struct {
	PublicCheckoutBaseURL string
	TenancyMode           string
	DedicatedTenantID     string

	VaultAddr            string
	VaultToken           string
	CryptoTransitMount   string
	WebhookSecretKVMount string
	ApprovalPolicyKey    ed25519.PublicKey
	ApprovalPolicyJWS    map[string]string
}

// loadServerSettings reads and validates every server setting of the
// configuration, naming each bad variable.
func loadServerSettings() (serverSettings, error) {
	env := envconfig.New()
	settings := serverSettings{
		PublicCheckoutBaseURL: env.Required("PUBLIC_CHECKOUT_BASE_URL"),
		TenancyMode:           env.String("TENANCY_MODE", ""),
		DedicatedTenantID:     env.String("DEDICATED_TENANT_ID", ""),

		VaultAddr:            env.Required("VAULT_ADDR"),
		VaultToken:           strings.TrimSpace(env.String("VAULT_TOKEN", "")),
		CryptoTransitMount:   env.String("CRYPTO_TRANSIT_MOUNT", "transit"),
		WebhookSecretKVMount: env.String("WEBHOOK_SECRET_KV_MOUNT", "secret"),
		ApprovalPolicyKey:    loadApprovalPolicyKey(env, "CRYPTO_APPROVAL_POLICY_PUBLIC_KEY"),
		ApprovalPolicyJWS:    loadApprovalPolicyJWSByTenant(env),
	}
	if env.String("APP_ENV", "development") == "production" && settings.VaultToken != "" {
		env.Fail("VAULT_TOKEN", errors.New("is forbidden in production; use tokenless Vault Proxy mode"))
	}
	if err := env.Err(); err != nil {
		return serverSettings{}, err
	}
	return settings, nil
}

func loadApprovalPolicyKey(env *envconfig.Reader, name string) ed25519.PublicKey {
	raw := strings.TrimSpace(env.String(name, ""))
	if raw == "" {
		env.Fail(name, errors.New("is required"))
		return nil
	}
	key, err := domaincrypto.ParseEd25519PublicKey(raw)
	if err != nil {
		env.Fail(name, err)
		return nil
	}
	return key
}

// loadApprovalPolicyJWSByTenant reads either one default policy bundle or a
// JSON object of tenant labels to policy bundles; exactly one must be set.
func loadApprovalPolicyJWSByTenant(env *envconfig.Reader) map[string]string {
	const single, multi = "CRYPTO_APPROVAL_POLICY_BUNDLE_JWS", "CRYPTO_APPROVAL_POLICY_BUNDLES_JWS"
	rawSingle := strings.TrimSpace(env.String(single, ""))
	rawMap := strings.TrimSpace(env.String(multi, ""))
	switch {
	case rawSingle != "" && rawMap != "":
		env.Fail(multi, fmt.Errorf("configure only one of %s or %s", single, multi))
		return nil
	case rawMap != "":
		var policies map[string]string
		if err := json.Unmarshal([]byte(rawMap), &policies); err != nil {
			env.Fail(multi, fmt.Errorf("must be a JSON object of tenant labels to policy JWS values: %w", err))
			return nil
		}
		if len(policies) == 0 {
			env.Fail(multi, errors.New("must contain at least one signed policy"))
			return nil
		}
		return policies
	case rawSingle == "":
		env.Fail(single, errors.New("is required"))
		return nil
	default:
		return map[string]string{"default": rawSingle}
	}
}

// Server builds the crypto configuration's part of the API server: public
// checkout, the merchant API and portal, Vault-backed approval signing and
// webhook secrets, its registers and its integration connection tests.
func (c cryptoConfiguration) Server(ctx context.Context, env configuration.ServerEnv) (configuration.Server, error) {
	settings, err := loadServerSettings()
	if err != nil {
		return configuration.Server{}, err
	}

	// MerchantUserRepo feeds merchant portal access scopes into session JWTs.
	merchantUserRepo := crypto_catalog_repo.NewMerchantUserRepo()

	checkoutRefs, err := checkout.NewService(checkout.Config{
		Repo:              crypto_repo.NewCheckoutReferenceRepo(env.MetaPool),
		Registry:          env.Tenants,
		PublicBaseURL:     settings.PublicCheckoutBaseURL,
		TenancyMode:       settings.TenancyMode,
		DedicatedTenantID: settings.DedicatedTenantID,
	})
	if err != nil {
		return configuration.Server{}, fmt.Errorf("create checkout reference service: %w", err)
	}

	merchantAPIKeyRepo := crypto_catalog_repo.NewMerchantAPIKeyRepo(env.MetaPool)
	merchantInvoiceRepo := crypto_document_repo.NewCryptoInvoiceRepo()
	merchantLifecyclePublisher := crypto_repo.NewLifecycleWebhookOutbox()
	merchantInvoiceSvc := newMerchantInvoiceService(merchantInvoiceRepo, env.Numerator, merchantLifecyclePublisher)
	checkoutInvoiceSvc := cryptoapp.NewCheckoutInvoiceService(merchantInvoiceSvc, checkoutRefs)
	merchantInvoiceLifecycle := domaincrypto.NewInvoiceLifecycleService(
		checkoutInvoiceSvc,
		crypto_document_repo.NewCryptoPaymentRepo(),
		merchantLifecyclePublisher,
		nil,
		nil,
	)
	merchantInvoiceAppSvc, err := cryptoapp.NewMerchantInvoiceService(cryptoapp.MerchantInvoiceServiceConfig{
		InvoiceService:      checkoutInvoiceSvc,
		InvoiceRepo:         merchantInvoiceRepo,
		ReferenceRepo:       merchantInvoiceRepo,
		Events:              env.EventLog,
		Settings:            crypto_repo.NewMerchantInvoiceSettingsRepo(),
		InvoiceLifecycle:    merchantInvoiceLifecycle,
		ActivationLifecycle: merchantInvoiceLifecycle,
		CheckoutRefs:        checkoutInvoiceSvc,
		IsUniqueViolation:   postgres.IsUniqueViolation,
	})
	if err != nil {
		return configuration.Server{}, fmt.Errorf("create merchant invoice application service: %w", err)
	}
	portalDashboardRepo := portal_repo.NewDashboardRepo()

	approvalVaultClient, err := vaultclient.New(vaultclient.Config{Addr: settings.VaultAddr, Token: settings.VaultToken})
	if err != nil {
		return configuration.Server{}, fmt.Errorf("create approval Vault client: %w", err)
	}
	approvalSignerProvider, err := approval_signer.NewTransitProvider(ctx, approval_signer.TransitProviderConfig{
		Client:          approvalVaultClient,
		Mount:           settings.CryptoTransitMount,
		PolicyPublicKey: settings.ApprovalPolicyKey,
		PolicyJWS:       settings.ApprovalPolicyJWS,
	})
	if err != nil {
		return configuration.Server{}, fmt.Errorf("create money movement approval signer provider: %w", err)
	}

	paymentLinkSvc, err := cryptoapp.NewPaymentLinkService(cryptoapp.PaymentLinkServiceConfig{
		Repo: portalDashboardRepo,
		Refs: checkoutRefs,
	})
	if err != nil {
		return configuration.Server{}, fmt.Errorf("create payment link service: %w", err)
	}
	chainCapabilities, err := chainbootstrap.NewFamilyCapabilityRegistry()
	if err != nil {
		return configuration.Server{}, fmt.Errorf("compose blockchain family capabilities: %w", err)
	}
	webhookVaultClient, err := vaultclient.New(vaultclient.Config{Addr: settings.VaultAddr, Token: settings.VaultToken})
	if err != nil {
		return configuration.Server{}, fmt.Errorf("create webhook Vault client: %w", err)
	}
	webhookSecretStore, err := webhooksecret.NewVaultStore(webhookVaultClient, settings.WebhookSecretKVMount)
	if err != nil {
		return configuration.Server{}, fmt.Errorf("create webhook secret store: %w", err)
	}

	services := Services{CheckoutRefs: checkoutRefs, MoneyMovementSigners: approvalSignerProvider}
	catalog := c.IntegrationCatalog()
	return configuration.Server{
		Register:       func(reg *v1.FactoryRegistry) { register(reg, services) },
		AccessScopes:   merchant.NewPortalAccessScopes(merchantUserRepo),
		PostingModules: []posting.Module{cryptoregisters.New()},
		IntegrationConnectionTests: integrationapp.NewConnectionTestService(crypto_integration_repo.NewConnectionTargets(), transport.ConnectionProber{
			Accounts: integration_repo.NewRuntime(catalog), Profiles: cryptoprofiles.Registry(),
			EndpointProbe: crypto_integration_repo.RPCProbe, EndpointTransport: crypto_integration_repo.RPCTransport,
		}),
		RouteModules: []v1.RouteModule{cryptohttp.New(cryptohttp.Config{
			CheckoutRefs:                 checkoutRefs,
			MoneyMovementApprovalSigners: approvalSignerProvider,
			ChainCapabilities:            chainCapabilities,
			MerchantAPIKeyRepo:           merchantAPIKeyRepo,
			MerchantUserRepo:             merchantUserRepo,
			MerchantInvoiceSvc:           checkoutInvoiceSvc,
			MerchantInvoiceAppSvc:        merchantInvoiceAppSvc,
			MerchantWebhookSecretStore:   webhookSecretStore,
			PaymentLinkSvc:               paymentLinkSvc,
			PortalDashboardRepo:          portalDashboardRepo,
		})},
	}, nil
}

// newMerchantInvoiceService builds the CryptoInvoice service behind the public
// /merchant/v1/ API. It leases the receiving wallet through the same step as
// the ERP CryptoInvoice registration (NewInvoiceWalletLease), so an invoice
// created here is never stored with wallet_id = NULL, and it queues the
// invoice.created lifecycle webhook in the creation transaction.
func newMerchantInvoiceService(
	repo crypto_invoice.Repository,
	num numerator.Generator,
	lifecycle *crypto_repo.LifecycleWebhookOutbox,
) *crypto_invoice.Service {
	svc := crypto_invoice.NewService(
		repo,
		nil, // posting engine — invoices are not posted at creation
		num,
		nil, // TxManager from context
	)
	svc.Hooks().OnBeforeCreate(NewInvoiceWalletLease().Apply)
	svc.OnCreatedInTransaction(func(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
		invoiceID := doc.ID
		return lifecycle.PublishLifecycleWebhook(ctx, domaincrypto.LifecycleWebhookEvent{
			Event: domaincrypto.WebhookInvoiceCreated, AggregateID: doc.ID,
			MerchantID: doc.MerchantID, InvoiceID: &invoiceID,
			ResourceVersion: doc.Version, OccurredAt: time.Now().UTC(),
			Data: domaincrypto.InvoiceWebhookData(doc, nil),
		})
	})
	return svc
}
