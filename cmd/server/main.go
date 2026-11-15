// Package main is the entry point for the Metapus API server.
// Multi-tenant architecture: Database-per-Tenant.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"metapus/extensions"
	"metapus/internal/application/checkout"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/application/integrationapp"
	"metapus/internal/content"
	"metapus/internal/core/json"
	"metapus/internal/core/security"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/catalogs/wallet"
	domaincrypto "metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/authjwt"
	"metapus/internal/infrastructure/authqr"
	chainbootstrap "metapus/internal/infrastructure/blockchain/bootstrap"
	"metapus/internal/infrastructure/crypto/approval_signer"
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
	"metapus/internal/platform/secretfile"
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
	// Initialize logger
	log, err := logger.New(logger.Config{
		Level:       getEnv("LOG_LEVEL", "info"),
		Development: getEnv("APP_ENV", "development") == "development",
	})
	if err != nil {
		fmt.Printf("failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	shutdownTracing, err := platformtelemetry.ConfigureTracing(ctx, "metapus-api", Version, os.Getenv)
	if err != nil {
		log.Fatalw("failed to initialize OTLP tracing", "error", err)
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
	metaDSN := mustEnv("META_DATABASE_URL")
	metaPool, err := postgres.OpenPool(ctx, metaDSN)
	if err != nil {
		log.Fatalw("failed to connect to meta database", "error", err)
	}
	defer metaPool.Close()

	if err := metaPool.Ping(ctx); err != nil {
		log.Fatalw("failed to ping meta database", "error", err)
	}
	log.Info("meta database connection established")

	// --- Tenant Registry and Manager ---
	registry := tenantdb.NewPostgresRegistry(metaPool)

	managerCfg := tenantdb.DefaultManagerConfig()
	managerCfg.DBUser = mustEnv("TENANT_DB_USER")
	managerCfg.DBPassword = mustEnv("TENANT_DB_PASSWORD")
	managerCfg.ConfigurePool = postgres.ConfigurePool

	// Optional configuration overrides
	if maxPools := getEnvInt("TENANT_MAX_POOLS", 100); maxPools > 0 {
		managerCfg.MaxTotalPools = maxPools
	}
	if maxConns := getEnvInt("TENANT_MAX_CONNS_PER_POOL", 10); maxConns > 0 {
		managerCfg.MaxConnsPerTenant = int32(maxConns)
	}
	if idleTimeout := getEnvDuration("TENANT_POOL_IDLE_TIMEOUT", 30*time.Minute); idleTimeout > 0 {
		managerCfg.PoolIdleTimeout = idleTimeout
	}

	tenantManager := tenantdb.NewManager(managerCfg, registry, log)
	defer tenantManager.Close()

	log.Infow("tenant manager initialized",
		"max_pools", managerCfg.MaxTotalPools,
		"max_conns_per_tenant", managerCfg.MaxConnsPerTenant,
		"idle_timeout", managerCfg.PoolIdleTimeout,
	)

	// Optional: Prewarm pools for known tenants
	if getEnv("PREWARM_POOLS", "false") == "true" {
		log.Info("prewarming tenant pools...")
		if err := tenantManager.PrewarmPools(ctx); err != nil {
			log.Warnw("failed to prewarm some pools", "error", err)
		}
	}

	// --- JWT Service ---
	jwtSecret := mustEnv("JWT_SECRET")
	jwtConfig := authjwt.DefaultJWTConfig(jwtSecret)
	jwtSvc := authjwt.NewJWTService(jwtConfig)

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
	authStateCache := auth.NewAuthStateCache(getEnvDuration("AUTH_STATE_CACHE_TTL", 5*time.Minute), getEnvInt("AUTH_STATE_CACHE_MAX_ENTRIES", 10000))
	accessValidator := auth.NewAccessTokenValidator(jwtSvc, sessionRepo, authStateCache)

	// MerchantUserRepo feeds merchant portal access scopes into auth.Service JWTs.
	// Created early so it can be shared with RouterConfig below.
	merchantUserRepo := crypto_catalog_repo.NewMerchantUserRepo()

	authConfig := loadAuthServiceConfig(log)
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
		authConfig,
		auth.WithQRCodeEncoder(authqr.Encoder{}),
		auth.WithAccessScopeProvider(merchant.NewPortalAccessScopes(merchantUserRepo)),
	)

	// --- Numerator Service ---
	numeratorSvc := numerator.New()

	// --- Security Profile Provider (cached) ---
	profileRepo := security_repo.NewProfileRepo()
	profileCacheTTL := getEnvDuration("SECURITY_PROFILE_CACHE_TTL", 5*time.Minute)
	profileProvider := security_profile.NewCachedProfileProvider(profileRepo, profileCacheTTL, getEnvInt("SECURITY_PROFILE_CACHE_MAX_ENTRIES", 10000))

	// --- CEL Policy Engine ---
	policyEngine, err := security.NewPolicyEngine()
	if err != nil {
		log.Fatal("failed to create policy engine", "error", err)
	}
	defer policyEngine.Stop()

	// --- Entity Registry (composition root) ---
	// Create factory registry and populate with built-in business content.

	// --- WebSocket Ticket Store ---
	wsTicketStore := auth.NewWSTicketStore()
	defer wsTicketStore.Stop()

	// --- Merchant Public API ---
	// MerchantAPIKeyRepo and a minimal CryptoInvoice service for /merchant/v1/ endpoints.
	// Uses the same wallet-lease hook as the ERP service (CryptoInvoiceRegistration.Build)
	// to ensure invoices created via the public API get a pool wallet assigned.
	checkoutRefs, err := checkout.NewService(checkout.Config{
		Repo:              crypto_repo.NewCheckoutReferenceRepo(metaPool),
		Registry:          registry,
		PublicBaseURL:     mustEnv("PUBLIC_CHECKOUT_BASE_URL"),
		TenancyMode:       getEnv("TENANCY_MODE", ""),
		DedicatedTenantID: getEnv("DEDICATED_TENANT_ID", ""),
	})
	if err != nil {
		log.Fatalw("failed to create checkout reference service", "error", err)
	}

	merchantAPIKeyRepo := crypto_catalog_repo.NewMerchantAPIKeyRepo(metaPool)
	// merchantUserRepo is created above (needed by auth.Service for JWT portal claims).
	merchantInvoiceRepo := crypto_document_repo.NewCryptoInvoiceRepo()
	merchantInvoiceSvc := crypto_invoice.NewService(
		merchantInvoiceRepo,
		nil, // posting engine — invoices are not posted at creation
		numeratorSvc,
		nil, // TxManager from context
	)

	// Register wallet-lease hook — same logic as content/document_registrations.go:191-208.
	// Without this, invoices created via /merchant/v1/ would have wallet_id = NULL.
	merchantTokenRepo := crypto_catalog_repo.NewTokenRepo()
	merchantWalletRepo := crypto_catalog_repo.NewWalletRepo()
	merchantWalletSvc := wallet.NewService(merchantWalletRepo, numeratorSvc)
	merchantAllocRepo := crypto_repo.NewWalletAllocationRepo()
	merchantLifecyclePublisher := crypto_repo.NewLifecycleWebhookOutbox()

	merchantInvoiceSvc.Hooks().OnBeforeCreate(func(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
		tok, err := merchantTokenRepo.GetByID(ctx, doc.TokenID)
		if err != nil {
			return fmt.Errorf("resolve token for wallet lease: %w", err)
		}
		w, err := merchantWalletSvc.LeaseForInvoice(ctx, doc.MerchantID, doc.ID, tok.NetworkID, tok.ID, doc.ExpiresAt)
		if err != nil {
			return fmt.Errorf("lease wallet for invoice: %w", err)
		}
		doc.WalletID = &w.ID
		if err := merchantAllocRepo.Open(ctx, domaincrypto.WalletAllocation{
			WalletID:   w.ID,
			InvoiceID:  doc.ID,
			MerchantID: doc.MerchantID,
			TokenID:    doc.TokenID,
			NetworkID:  tok.NetworkID,
		}); err != nil {
			return fmt.Errorf("record wallet allocation: %w", err)
		}
		return nil
	})
	merchantInvoiceSvc.OnCreatedInTransaction(func(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
		invoiceID := doc.ID
		return merchantLifecyclePublisher.PublishLifecycleWebhook(ctx, domaincrypto.LifecycleWebhookEvent{
			Event: domaincrypto.WebhookInvoiceCreated, AggregateID: doc.ID,
			MerchantID: doc.MerchantID, InvoiceID: &invoiceID,
			ResourceVersion: doc.Version, OccurredAt: time.Now().UTC(),
			Data: domaincrypto.InvoiceWebhookData(doc, nil),
		})
	})

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
		log.Fatalw("failed to create merchant invoice application service", "error", err)
	}
	portalDashboardRepo := portal_repo.NewDashboardRepo()
	approvalSignerProvider := loadMoneyMovementApprovalSignerProvider(ctx, log)

	// Entity factories: built-in content (with its crypto services), then client extensions.
	factoryReg := v1.NewFactoryRegistry()
	content.RegisterDefaults(factoryReg, content.Services{CheckoutRefs: checkoutRefs, MoneyMovementSigners: approvalSignerProvider})
	extensions.Register(factoryReg)
	paymentLinkSvc, err := cryptoapp.NewPaymentLinkService(cryptoapp.PaymentLinkServiceConfig{
		Repo: portalDashboardRepo,
		Refs: checkoutRefs,
	})
	if err != nil {
		log.Fatalw("failed to create payment link service", "error", err)
	}
	chainCapabilities, err := chainbootstrap.NewFamilyCapabilityRegistry()
	if err != nil {
		log.Fatalw("failed to compose blockchain family capabilities", "error", err)
	}
	webhookVaultClient, err := vaultclient.New(vaultclient.Config{
		Addr: mustEnv("VAULT_ADDR"), Token: strings.TrimSpace(getEnv("VAULT_TOKEN", "")),
	})
	if err != nil {
		log.Fatalw("failed to create webhook Vault client", "error", err)
	}
	webhookSecretStore, err := webhooksecret.NewVaultStore(webhookVaultClient, getEnv("WEBHOOK_SECRET_KV_MOUNT", "secret"))
	if err != nil {
		log.Fatalw("failed to create webhook secret store", "error", err)
	}

	// --- Router ---
	var pdfClient *pdfrender.Client
	if endpoint := os.Getenv("PDF_RENDERER_URL"); endpoint != "" {
		pdfClient, err = pdfrender.NewClient(endpoint)
		if err != nil {
			log.Fatalw("invalid PDF renderer configuration", "error", err)
		}
	}
	var pdfRenderPort printing.PDFRenderer
	var pdfReady func(context.Context) error
	if pdfClient != nil {
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
		AdminMFAMode:               authConfig.AdminMFAMode,
		AdminSecurityMode:          authConfig.AdminSecurityMode,
		AdminAllowedCIDRs:          authConfig.AdminAllowedCIDRs,
		Numerator:                  numeratorSvc,
		ProfileProvider:            profileProvider,
		PolicyEngine:               policyEngine,
		EventLogRepo:               eventLogRepo,
		Registry:                   factoryReg,
		Version:                    Version,
		BuildTime:                  BuildTime,
		WSTicketStore:              wsTicketStore,
		IntegrationConnectionTests: integrationapp.NewConnectionTestService(crypto_integration_repo.NewConnectionTargets(), integrationhttp.ConnectionProber{Accounts: integration_repo.NewRuntime(), EndpointTransport: crypto_integration_repo.RPCTransport}),
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
	port := getEnv("APP_PORT", "8080")
	server := &http.Server{
		Addr:           ":" + port,
		Handler:        http.MaxBytesHandler(router, 10<<20), // 10 MiB global body limit
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MiB max header size
	}

	// Start server in goroutine
	go func() {
		log.Infow("server starting", "port", port, "mode", "multi-tenant")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalw("server failed", "error", err)
		}
	}()

	// --- Graceful shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down server...")

	// Give outstanding requests 30 seconds to complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalw("server forced to shutdown", "error", err)
	}

	log.Info("server stopped")
}

func getEnv(key, defaultValue string) string {
	value, err := secretfile.Get(key, defaultValue)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration for %s: %v\n", key, err)
		os.Exit(1)
	}
	return value
}

func mustEnv(key string) string {
	value := getEnv(key, "")
	if value == "" {
		fmt.Printf("required environment variable %s not set\n", key)
		os.Exit(1)
	}
	return value
}

func loadMoneyMovementApprovalSignerProvider(ctx context.Context, log *logger.Logger) domaincrypto.MoneyMovementApprovalSignerProvider {
	policyPublicKey := mustParseApprovalPublicKey(log, "CRYPTO_APPROVAL_POLICY_PUBLIC_KEY")
	vaultToken := strings.TrimSpace(getEnv("VAULT_TOKEN", ""))
	if getEnv("APP_ENV", "development") == "production" && vaultToken != "" {
		log.Fatalw("VAULT_TOKEN is forbidden in production; use tokenless Vault Proxy mode")
	}
	client, err := vaultclient.New(vaultclient.Config{
		Addr:  mustEnv("VAULT_ADDR"),
		Token: vaultToken,
	})
	if err != nil {
		log.Fatalw("failed to create approval Vault client", "error", err)
	}
	provider, err := approval_signer.NewTransitProvider(ctx, approval_signer.TransitProviderConfig{
		Client:          client,
		Mount:           getEnv("CRYPTO_TRANSIT_MOUNT", "transit"),
		PolicyPublicKey: policyPublicKey,
		PolicyJWS:       loadApprovalPolicyJWSByTenant(log),
	})
	if err != nil {
		log.Fatalw("failed to create money movement approval signer provider", "error", err)
	}
	return provider
}

func loadApprovalPolicyJWSByTenant(log *logger.Logger) map[string]string {
	rawSingle := strings.TrimSpace(getEnv("CRYPTO_APPROVAL_POLICY_BUNDLE_JWS", ""))
	rawMap := strings.TrimSpace(getEnv("CRYPTO_APPROVAL_POLICY_BUNDLES_JWS", ""))
	if rawSingle != "" && rawMap != "" {
		log.Fatalw("configure only one of CRYPTO_APPROVAL_POLICY_BUNDLE_JWS or CRYPTO_APPROVAL_POLICY_BUNDLES_JWS")
	}
	if rawMap != "" {
		var policies map[string]string
		if err := json.Unmarshal([]byte(rawMap), &policies); err != nil {
			log.Fatalw("CRYPTO_APPROVAL_POLICY_BUNDLES_JWS must be a JSON object of tenant labels to policy JWS values", "error", err)
		}
		if len(policies) == 0 {
			log.Fatalw("CRYPTO_APPROVAL_POLICY_BUNDLES_JWS must contain at least one signed policy")
		}
		return policies
	}
	if rawSingle == "" {
		log.Fatalw("CRYPTO_APPROVAL_POLICY_BUNDLE_JWS is required")
	}
	return map[string]string{"default": rawSingle}
}

func mustParseApprovalPublicKey(log *logger.Logger, envName string) ed25519.PublicKey {
	raw := strings.TrimSpace(getEnv(envName, ""))
	if raw == "" {
		log.Fatalw(envName + " is required")
	}
	key, err := domaincrypto.ParseEd25519PublicKey(raw)
	if err != nil {
		log.Fatalw("invalid "+envName, "error", err)
	}
	return key
}

func loadAuthServiceConfig(log *logger.Logger) auth.ServiceConfig {
	cfg := auth.DefaultServiceConfig()
	cfg.AdminMFAMode = parseMFAMode(getEnv("ADMIN_MFA_MODE", string(cfg.AdminMFAMode)), log)
	cfg.AdminMFAGracePeriod = time.Duration(getEnvInt("ADMIN_MFA_GRACE_PERIOD_HOURS", 168)) * time.Hour
	cfg.AdminStepUpTTL = time.Duration(getEnvInt("ADMIN_STEP_UP_TTL_SECONDS", 300)) * time.Second
	cfg.AdminSecurityMode = parseAdminSecurityMode(getEnv("ADMIN_SECURITY_MODE", string(cfg.AdminSecurityMode)), log)
	cfg.AdminAllowedCIDRs = parseCIDRs(getEnv("ADMIN_ALLOWED_CIDRS", ""), "ADMIN_ALLOWED_CIDRS", log)
	cfg.MFAIssuer = getEnv("AUTH_MFA_ISSUER", cfg.MFAIssuer)

	if raw := strings.TrimSpace(getEnv("AUTH_MFA_ENCRYPTION_KEY", "")); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			log.Fatalw("AUTH_MFA_ENCRYPTION_KEY must be base64-encoded 32 bytes", "error", err)
		}
		cfg.MFAEncryptionKey = key
	} else {
		log.Fatalw("AUTH_MFA_ENCRYPTION_KEY is required for MFA enrollment and step-up")
	}

	return cfg
}

func parseMFAMode(value string, log *logger.Logger) auth.MFAMode {
	switch auth.MFAMode(strings.ToLower(strings.TrimSpace(value))) {
	case auth.MFAModeOff:
		return auth.MFAModeOff
	case auth.MFAModeOptional:
		return auth.MFAModeOptional
	case auth.MFAModeRequired:
		return auth.MFAModeRequired
	default:
		log.Fatalw("invalid ADMIN_MFA_MODE", "value", value)
		return auth.MFAModeOff
	}
}

func parseAdminSecurityMode(value string, log *logger.Logger) auth.AdminSecurityMode {
	switch auth.AdminSecurityMode(strings.ToLower(strings.TrimSpace(value))) {
	case auth.AdminSecurityOff:
		return auth.AdminSecurityOff
	case auth.AdminSecurityWarn:
		return auth.AdminSecurityWarn
	case auth.AdminSecurityEnforce:
		return auth.AdminSecurityEnforce
	default:
		log.Fatalw("invalid ADMIN_SECURITY_MODE", "value", value)
		return auth.AdminSecurityOff
	}
}

func parseCIDRs(value, envName string, log *logger.Logger) []netip.Prefix {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		raw := strings.TrimSpace(part)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			addr, addrErr := netip.ParseAddr(raw)
			if addrErr != nil {
				log.Fatalw("invalid CIDR", "env", envName, "value", raw, "error", err)
			}
			prefix = netip.PrefixFrom(addr.Unmap(), addr.BitLen())
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
			return result
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}
