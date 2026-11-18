// Package main is the entry point for the Metapus API server.
// Multi-tenant architecture: Database-per-Tenant.
//
// main.go is the composition root: it builds the dependency graph and runs
// the HTTP server. Environment parsing lives in config.go and business rules
// live in the application and content packages.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"metapus/extensions"
	"metapus/internal/application/checkout"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/application/integrationapp"
	"metapus/internal/content"
	corenumerator "metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/catalogs/merchant"
	domaincrypto "metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/authjwt"
	"metapus/internal/infrastructure/authqr"
	chainbootstrap "metapus/internal/infrastructure/blockchain/bootstrap"
	"metapus/internal/infrastructure/crypto/approval_signer"
	"metapus/internal/infrastructure/crypto/cryptoprofiles"
	"metapus/internal/infrastructure/crypto/cryptoregisters"
	"metapus/internal/infrastructure/crypto/webhooksecret"
	"metapus/internal/infrastructure/http/cryptohttp"
	v1 "metapus/internal/infrastructure/http/v1"
	integrationhttp "metapus/internal/infrastructure/integrations"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/pdfrender"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/auth_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_integration_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/integration_repo"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
	"metapus/internal/infrastructure/storage/postgres/security_repo"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/internal/infrastructure/vaultclient"
	platformtelemetry "metapus/internal/platform/telemetry"
	"metapus/pkg/logger"
)

// Version information — set via ldflags at build time:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/server
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration:\n%v\n", err)
		os.Exit(1)
	}
	log, err := logger.New(logger.Config{Level: cfg.LogLevel, Development: cfg.development()})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	if err := run(context.Background(), cfg, log); err != nil {
		log.Errorw("server failed", "error", err)
		_ = log.Sync()
		os.Exit(1)
	}
}

// run builds the server and serves until SIGINT/SIGTERM or a listener
// failure. Every resource it opens is closed by a defer, so an error returned
// from any step still releases what earlier steps acquired. ctx is the
// lifetime of the built components; it is not cancelled by the signal, so
// background work keeps running while in-flight requests drain.
func run(ctx context.Context, cfg config, log *logger.Logger) error {
	shutdownTracing, err := platformtelemetry.ConfigureTracing(ctx, "metapus-api", Version, os.Getenv)
	if err != nil {
		return fmt.Errorf("initialize OTLP tracing: %w", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if shutdownErr := shutdownTracing(shutdownCtx); shutdownErr != nil {
			log.Errorw("failed to flush OTLP traces", "error", shutdownErr)
		}
	}()
	log.Infow("starting metapus server",
		"version", Version,
		"build_time", BuildTime,
		"mode", "multi-tenant",
	)

	// --- Meta-database connection ---
	metaPool, err := postgres.OpenPool(ctx, cfg.MetaDatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to meta database: %w", err)
	}
	defer metaPool.Close()

	if err := metaPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping meta database: %w", err)
	}
	log.Info("meta database connection established")

	// --- Tenant Registry and Manager ---
	registry := tenantdb.NewPostgresRegistry(metaPool)

	managerCfg := tenantdb.DefaultManagerConfig()
	managerCfg.DBUser = cfg.TenantDBUser
	managerCfg.DBPassword = cfg.TenantDBPassword
	managerCfg.ConfigurePool = postgres.ConfigurePool
	managerCfg.MaxTotalPools = cfg.TenantMaxPools
	managerCfg.MaxConnsPerTenant = int32(cfg.TenantMaxConnsPerPool)
	managerCfg.PoolIdleTimeout = cfg.TenantPoolIdleTimeout

	tenantManager := tenantdb.NewManager(managerCfg, registry, log)
	defer tenantManager.Close()

	log.Infow("tenant manager initialized",
		"max_pools", managerCfg.MaxTotalPools,
		"max_conns_per_tenant", managerCfg.MaxConnsPerTenant,
		"idle_timeout", managerCfg.PoolIdleTimeout,
	)

	if cfg.PrewarmPools {
		log.Info("prewarming tenant pools...")
		if err := tenantManager.PrewarmPools(ctx); err != nil {
			log.Warnw("failed to prewarm some pools", "error", err)
		}
	}

	// --- JWT Service ---
	jwtSvc := authjwt.NewJWTService(authjwt.DefaultJWTConfig(cfg.JWTSecret))

	// --- Auth Service ---
	// Note: Auth repos will get TxManager from context per-request
	userRepo := auth_repo.NewUserRepo()
	roleRepo := auth_repo.NewRoleRepo()
	permRepo := auth_repo.NewPermissionRepo()
	tokenRepo := auth_repo.NewTokenRepo()
	sessionRepo := auth_repo.NewSessionRepo()
	mfaRepo := auth_repo.NewMFARepo()
	eventLogRepo := postgres.NewEventLogRepo()
	notificationRepo := postgres.NewNotificationRepo()
	authStateCache := auth.NewAuthStateCache(cfg.AuthStateCacheTTL, cfg.AuthStateCacheMaxEntries)
	accessValidator := auth.NewAccessTokenValidator(jwtSvc, sessionRepo, authStateCache)

	// MerchantUserRepo feeds merchant portal access scopes into auth.Service JWTs.
	// Created early so it can be shared with RouterConfig below.
	merchantUserRepo := crypto_catalog_repo.NewMerchantUserRepo()

	authSvc := auth.NewService(
		userRepo,
		roleRepo,
		permRepo,
		tokenRepo,
		mfaRepo,
		sessionRepo,
		authStateCache,
		nil, // TxManager will come from context
		jwtSvc,
		eventLogRepo,
		notificationRepo,
		cfg.Auth,
		auth.WithQRCodeEncoder(authqr.Encoder{}),
		auth.WithAccessScopeProvider(merchant.NewPortalAccessScopes(merchantUserRepo)),
	)

	// --- Numerator Service ---
	numeratorSvc := numerator.New()

	// --- Security Profile Provider (cached) ---
	profileProvider := security_profile.NewCachedProfileProvider(
		security_repo.NewProfileRepo(),
		cfg.SecurityProfileCacheTTL,
		cfg.SecurityProfileCacheMaxEntries,
	)

	// --- CEL Policy Engine ---
	policyEngine, err := security.NewPolicyEngine()
	if err != nil {
		return fmt.Errorf("create policy engine: %w", err)
	}
	defer policyEngine.Stop()

	// --- WebSocket Ticket Store ---
	wsTicketStore := auth.NewWSTicketStore()
	defer wsTicketStore.Stop()

	// --- Merchant Public API ---
	checkoutRefs, err := checkout.NewService(checkout.Config{
		Repo:              crypto_repo.NewCheckoutReferenceRepo(metaPool),
		Registry:          registry,
		PublicBaseURL:     cfg.PublicCheckoutBaseURL,
		TenancyMode:       cfg.TenancyMode,
		DedicatedTenantID: cfg.DedicatedTenantID,
	})
	if err != nil {
		return fmt.Errorf("create checkout reference service: %w", err)
	}

	merchantAPIKeyRepo := crypto_catalog_repo.NewMerchantAPIKeyRepo(metaPool)
	merchantInvoiceRepo := crypto_document_repo.NewCryptoInvoiceRepo()
	merchantLifecyclePublisher := crypto_repo.NewLifecycleWebhookOutbox()
	merchantInvoiceSvc := newMerchantInvoiceService(merchantInvoiceRepo, numeratorSvc, merchantLifecyclePublisher)
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
		Events:              eventLogRepo,
		Settings:            crypto_repo.NewMerchantInvoiceSettingsRepo(),
		InvoiceLifecycle:    merchantInvoiceLifecycle,
		ActivationLifecycle: merchantInvoiceLifecycle,
		CheckoutRefs:        checkoutInvoiceSvc,
		IsUniqueViolation:   postgres.IsUniqueViolation,
	})
	if err != nil {
		return fmt.Errorf("create merchant invoice application service: %w", err)
	}
	portalDashboardRepo := portal_repo.NewDashboardRepo()

	// --- Vault-backed crypto services ---
	approvalVaultClient, err := vaultclient.New(vaultclient.Config{Addr: cfg.VaultAddr, Token: cfg.VaultToken})
	if err != nil {
		return fmt.Errorf("create approval Vault client: %w", err)
	}
	approvalSignerProvider, err := approval_signer.NewTransitProvider(ctx, approval_signer.TransitProviderConfig{
		Client:          approvalVaultClient,
		Mount:           cfg.CryptoTransitMount,
		PolicyPublicKey: cfg.ApprovalPolicyKey,
		PolicyJWS:       cfg.ApprovalPolicyJWS,
	})
	if err != nil {
		return fmt.Errorf("create money movement approval signer provider: %w", err)
	}

	// Entity factories: built-in content (with its crypto services), then client extensions.
	factoryReg := content.Compose(content.Services{CheckoutRefs: checkoutRefs, MoneyMovementSigners: approvalSignerProvider}, extensions.Register)
	paymentLinkSvc, err := cryptoapp.NewPaymentLinkService(cryptoapp.PaymentLinkServiceConfig{
		Repo: portalDashboardRepo,
		Refs: checkoutRefs,
	})
	if err != nil {
		return fmt.Errorf("create payment link service: %w", err)
	}
	chainCapabilities, err := chainbootstrap.NewFamilyCapabilityRegistry()
	if err != nil {
		return fmt.Errorf("compose blockchain family capabilities: %w", err)
	}
	webhookVaultClient, err := vaultclient.New(vaultclient.Config{Addr: cfg.VaultAddr, Token: cfg.VaultToken})
	if err != nil {
		return fmt.Errorf("create webhook Vault client: %w", err)
	}
	webhookSecretStore, err := webhooksecret.NewVaultStore(webhookVaultClient, cfg.WebhookSecretKVMount)
	if err != nil {
		return fmt.Errorf("create webhook secret store: %w", err)
	}

	// --- Router ---
	var pdfRenderPort printing.PDFRenderer
	var pdfReady func(context.Context) error
	if cfg.PDFRendererURL != "" {
		pdfClient, err := pdfrender.NewClient(cfg.PDFRendererURL)
		if err != nil {
			return fmt.Errorf("invalid PDF renderer configuration: %w", err)
		}
		pdfRenderPort = pdfClient
		pdfReady = pdfClient.Ready
	}
	router := v1.NewRouter(v1.RouterConfig{
		PostingModules:             []posting.Module{cryptoregisters.New()},
		PDFRenderer:                pdfRenderPort,
		PDFReady:                   pdfReady,
		TenantManager:              tenantManager,
		MetaPool:                   metaPool,
		Logger:                     log,
		JWTValidator:               accessValidator,
		AuthSvc:                    authSvc,
		MFARepo:                    mfaRepo,
		AdminMFAMode:               cfg.Auth.AdminMFAMode,
		AdminSecurityMode:          cfg.Auth.AdminSecurityMode,
		AdminAllowedCIDRs:          cfg.Auth.AdminAllowedCIDRs,
		Numerator:                  numeratorSvc,
		ProfileProvider:            profileProvider,
		PolicyEngine:               policyEngine,
		EventLogRepo:               eventLogRepo,
		Registry:                   factoryReg,
		Version:                    Version,
		BuildTime:                  BuildTime,
		WSTicketStore:              wsTicketStore,
		IntegrationConnectionTests: integrationapp.NewConnectionTestService(crypto_integration_repo.NewConnectionTargets(), integrationhttp.ConnectionProber{Accounts: integration_repo.NewRuntime(cryptointegrations.Catalog()), Profiles: cryptoprofiles.Registry(), EndpointProbe: crypto_integration_repo.RPCProbe, EndpointTransport: crypto_integration_repo.RPCTransport}),
		IntegrationCatalog:         cryptointegrations.Catalog(),
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
	})

	// --- HTTP Server ---
	server := &http.Server{
		Addr:           ":" + cfg.Port,
		Handler:        http.MaxBytesHandler(router, 10<<20), // 10 MiB global body limit
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MiB max header size
	}
	return serve(ctx, server, log)
}

// newMerchantInvoiceService builds the CryptoInvoice service behind the public
// /merchant/v1/ API. It leases the receiving wallet through the same step as
// the ERP CryptoInvoice registration (content.NewInvoiceWalletLease), so an
// invoice created here is never stored with wallet_id = NULL, and it queues
// the invoice.created lifecycle webhook in the creation transaction.
func newMerchantInvoiceService(
	repo crypto_invoice.Repository,
	num corenumerator.Generator,
	lifecycle *crypto_repo.LifecycleWebhookOutbox,
) *crypto_invoice.Service {
	svc := crypto_invoice.NewService(
		repo,
		nil, // posting engine — invoices are not posted at creation
		num,
		nil, // TxManager from context
	)
	svc.Hooks().OnBeforeCreate(content.NewInvoiceWalletLease().Apply)
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

// serve runs server until SIGINT/SIGTERM, then gives outstanding requests
// 30 seconds to finish. A listener failure is returned instead of exiting, so
// run's deferred cleanup still executes.
func serve(ctx context.Context, server *http.Server, log *logger.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Infow("server starting", "addr", server.Addr, "mode", "multi-tenant")
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("server stopped")
	return nil
}
