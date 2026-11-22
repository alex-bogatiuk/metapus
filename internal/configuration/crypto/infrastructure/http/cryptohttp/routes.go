package cryptohttp

import (
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/application/dashboardapp"
	"metapus/internal/configuration/crypto/application/portalapp"
	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/registers/crypto_fee"
	"metapus/internal/configuration/crypto/domain/registers/crypto_merchant_balance"
	"metapus/internal/configuration/crypto/domain/registers/crypto_receipt_suspense"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptohandlers"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptomiddleware"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_catalog_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_integration_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/dashboard_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/money_movement_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/portal_repo"
	"metapus/pkg/sdk/command"
	"metapus/pkg/sdk/http/handlers"
	"metapus/pkg/sdk/http/middleware"
	"metapus/pkg/sdk/http/v1"
	"metapus/pkg/sdk/numerator"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/posting"
	"metapus/pkg/sdk/registers"
	"metapus/pkg/sdk/registers/exchange_rate"
	"metapus/pkg/sdk/tenant"
)

func (m *Module) registerWalletPoolRoutes(rg *gin.RouterGroup, cfg v1.RouterConfig) {
	m.registerUnidentifiedReceiptRoutes(rg, cfg)
	repo := crypto_repo.NewWalletPoolRepo()
	svc := crypto.NewWalletPoolAdminService(repo, m.cfg.ChainCapabilities, cfg.EventLogRepo)
	handler := cryptohandlers.NewWalletPoolHandler(handlers.NewBaseHandler(), svc)
	handler.RegisterRoutes(
		rg,
		middleware.SensitiveActionGuard(
			cfg.MFARepo,
			"crypto.wallet_pool.policy.update",
			cfg.EventLogRepo,
		),
	)
}

// registerUnidentifiedReceiptRoutes mounts the journal of unidentified custody
// receipts. Merchant credits move only suspense, merchant balance and fee, so
// their posting engine carries just those three registers.
func (m *Module) registerUnidentifiedReceiptRoutes(rg *gin.RouterGroup, cfg v1.RouterConfig) {
	engine := posting.NewLockingEngine(postgres.NewDocLocker())
	engine.AddVisitor(&cryptoposting.CryptoReceiptSuspenseVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoMerchantBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoFeeVisitor{})
	engine.AddRecorder(cryptoposting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(crypto_register_repo.NewCryptoReceiptSuspenseRepo())))
	engine.AddRecorder(cryptoposting.NewCryptoMerchantBalanceRecorder(crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo())))
	engine.AddRecorder(cryptoposting.NewCryptoFeeRecorder(crypto_fee.NewService(crypto_register_repo.NewCryptoFeeRepo())))
	txm := postgres.NewContextTxManager()
	service := cryptoapp.NewMerchantCreditService(txm, crypto_repo.NewMerchantCreditRepo(),
		crypto.NewFeeConfigResolver(crypto_repo.NewFeeScheduleRepo()), engine, cfg.Numerator, postgres.NewOutboxPublisher(), time.Now)
	executor := command.NewExecutor(txm, postgres.NewCommandRepository())
	commands := cryptoapp.NewMerchantCreditCommands(service, executor)
	fundings := cryptoapp.NewPlatformFundingCommands(cryptoapp.NewPlatformFundingService(txm, crypto_repo.NewPlatformFundingRepo(),
		engine, cfg.Numerator, postgres.NewOutboxPublisher(), time.Now), executor)
	journal := cryptoapp.NewReceiptJournal(crypto_repo.NewUnidentifiedReceiptRepo(), crypto_repo.NewWalletFundingJournalRepo())
	cryptohandlers.NewUnidentifiedReceiptHandler(handlers.NewBaseHandler(), journal, commands, fundings).
		RegisterRoutes(rg, middleware.SensitiveActionGuard(cfg.MFARepo, "crypto.receipt.credit", cfg.EventLogRepo))
}

func (m *Module) registerCryptoNetworkRoutes(rg *gin.RouterGroup, cfg v1.RouterConfig) {
	cryptohandlers.NewCryptoRPCConfigurationHandler(cryptoapp.NewRPCConfigurationService(crypto_integration_repo.NewRPCConfigurationRepo(os.Getenv("APP_ENV"), cfg.PolicyEngine), tenant.ContextTxManager{}, cfg.EventLogRepo)).RegisterRoutes(rg, middleware.SensitiveActionGuard(cfg.MFARepo, "system.integrations.write", cfg.EventLogRepo))
	dashboardSvc := dashboardapp.NewService(dashboard_repo.New(), exchange_rate.NewService(registers.NewExchangeRateRepo()), registers.RateSnapshot)
	cryptohandlers.NewAdminDashboardHandler(dashboardSvc).RegisterRoutes(rg)
	repo := crypto_repo.NewNetworkPolicyRepo()
	svc := crypto.NewNetworkPolicyAdminService(repo, m.cfg.ChainCapabilities, cfg.EventLogRepo)
	shadow := crypto.NewShadowControlAdminService(crypto_repo.NewShadowControlRepo(), cfg.EventLogRepo)
	handler := cryptohandlers.NewCryptoNetworkHandler(handlers.NewBaseHandler(), svc, shadow)
	handler.RegisterRoutes(
		rg,
		middleware.SensitiveActionGuard(
			cfg.MFARepo,
			"crypto.network.policy.update",
			cfg.EventLogRepo,
		),
	)
}

// registerMerchantPublicRoutes registers the /merchant/v1/ group.
//
// Auth: X-Api-Key header. Tenant is resolved from the meta-database key index.
// These routes are completely isolated from the JWT-authenticated /api/v1/ group.
func (m *Module) registerMerchantPublicRoutes(rc v1.RouteContext) {
	router, cfg := rc.Root, rc.Config
	moneyMovementApprovals := money_movement_repo.New()
	networkPolicyRepo := portal_repo.NewMerchantNetworkPolicyRepo()
	if m.cfg.MerchantAPIKeyRepo == nil {
		return
	}
	if cfg.MetaPool == nil {
		panic("merchant public API requires v1.RouterConfig.MetaPool")
	}
	if m.cfg.MerchantInvoiceSvc == nil {
		panic("merchant public API requires cryptohttp.Config.MerchantInvoiceSvc")
	}
	if m.cfg.MerchantInvoiceAppSvc == nil {
		panic("merchant public API requires cryptohttp.Config.MerchantInvoiceAppSvc")
	}

	webhookRepo := crypto_repo.NewMerchantWebhookRepo()
	invoiceHandler := cryptohandlers.NewMerchantInvoiceHandler(
		m.cfg.MerchantInvoiceSvc, m.cfg.MerchantInvoiceAppSvc, m.cfg.CheckoutRefs,
		webhookRepo, cryptoapp.NewMerchantCryptoQueryService(crypto_repo.NewMerchantCryptoQueryRepo()),
	)
	apiKeyHandler := cryptohandlers.NewMerchantAPIKeyHandler(m.cfg.MerchantAPIKeyRepo)

	walletRepo := crypto_catalog_repo.NewWalletRepo()
	walletSvc := wallet.NewService(walletRepo, numerator.Noop())
	merchantCryptoQueries := cryptoapp.NewMerchantCryptoQueryService(crypto_repo.NewMerchantCryptoQueryRepo())
	addressHandler := cryptohandlers.NewMerchantAddressHandler(walletSvc, merchantCryptoQueries)

	// /merchant/v1/ — public merchant API (API-key auth)
	merchantBase := router.Group("/merchant/v1")
	merchantBase.Use(cryptomiddleware.MerchantAPIKey(m.cfg.MerchantAPIKeyRepo, cfg.TenantManager, cfg.MetaPool))
	merchantBase.Use(cryptomiddleware.MerchantNetworkAccess(networkPolicyRepo, merchant.NetworkSurfaceAPI))
	merchantBase.PATCH("/invoices/:id/reference",
		cryptomiddleware.RequireMerchantScope(merchant.ScopeInvoiceCreate), invoiceHandler.AuthorizeInvoiceReference,
		middleware.CommandIdempotency(true), invoiceHandler.BindInvoiceReference)
	merchantV1 := merchantBase.Group("")
	merchantV1.Use(middleware.CommandIdempotency(true))
	{
		merchantV1.GET("/currencies", invoiceHandler.ListCurrencies)

		invoices := merchantV1.Group("/invoices")
		{
			invoices.GET("", invoiceHandler.ListInvoices)
			invoices.POST("", invoiceHandler.CreateInvoice)
			invoices.GET("/:id", invoiceHandler.GetInvoice)
			invoices.POST("/:id/cancel", invoiceHandler.CancelInvoice)
		}

		addresses := merchantV1.Group("/addresses")
		{
			addresses.POST("", addressHandler.CreateAddress)
		}

		if m.cfg.MerchantWebhookSecretStore != nil {
			webhookHandler := cryptohandlers.NewMerchantWebhookHandler(webhookRepo, m.cfg.MerchantWebhookSecretStore, crypto_repo.NewWebhookDeliveryRepo())
			endpoints := merchantV1.Group("/webhook-endpoints")
			{
				endpoints.POST("", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.CreateEndpoint)
				endpoints.GET("", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEndpointRead), webhookHandler.ListEndpoints)
				endpoints.GET("/:id", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEndpointRead), webhookHandler.GetEndpoint)
				endpoints.PATCH("/:id", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.UpdateEndpoint)
				endpoints.DELETE("/:id", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.DeleteEndpoint)
				endpoints.POST("/:id/rotate-secret", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.RotateSecret)
				endpoints.POST("/:id/test", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.TestEndpoint)
			}
			events := merchantV1.Group("/events")
			{
				events.GET("", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEventRead), webhookHandler.ListEvents)
				events.GET("/:id/deliveries", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEventRead), webhookHandler.ListEventDeliveries)
				events.POST("/:id/redeliver", cryptomiddleware.RequireMerchantScope(merchant.ScopeWebhookEventRedeliver), webhookHandler.RedeliverEvent)
			}
		}
	}

	// /api/v1/merchant-admin/ — admin API for managing merchant API keys and users (JWT auth).
	// Security chain is the main protected group's (RouteContext.ProtectedGroup):
	//   TenantDB → Auth → AdminGuard → SecurityContext → RequireMerchantAccess → RequirePermission(per route)
	// CWE-639 fix: RequireMerchantAccess validates that the authenticated user
	// has a Manager+ association with the target :merchantId. Global admins bypass.
	merchantAdmin := rc.ProtectedGroup("/api/v1/merchant-admin")
	merchantAdmin.Use(middleware.CommandIdempotency(true))
	requireStepUp := func(actionCode string) gin.HandlerFunc {
		return middleware.SensitiveActionGuard(cfg.MFARepo, actionCode, cfg.EventLogRepo)
	}
	{
		// Sub-group with ownership check: all routes under /merchants/:merchantId
		// require the user to be associated with that merchant (Owner or Manager).
		merchantScoped := merchantAdmin.Group("/merchants/:merchantId")
		if m.cfg.MerchantUserRepo != nil {
			merchantScoped.Use(cryptomiddleware.RequireMerchantAccess(m.cfg.MerchantUserRepo, merchant.MerchantRoleManager))
		}

		// ─── API Keys ────────────────────────────────────────────────────────
		merchantScoped.POST("/api-keys",
			middleware.RequirePermission("merchant_api_keys:create"),
			requireStepUp("merchant.api_keys.create"),
			apiKeyHandler.CreateKey,
		)
		merchantScoped.GET("/api-keys",
			middleware.RequirePermission("merchant_api_keys:read"),
			apiKeyHandler.ListKeys,
		)
		merchantScoped.DELETE("/api-keys/:keyId",
			middleware.RequirePermission("merchant_api_keys:delete"),
			requireStepUp("merchant.api_keys.delete"),
			apiKeyHandler.RevokeKey,
		)

		// ─── Merchant Users ───────────────────────────────────────────────────
		if m.cfg.MerchantUserRepo != nil {
			userHandler := cryptohandlers.NewMerchantUserHandler(portalapp.NewMerchantUserService(m.cfg.MerchantUserRepo, cfg.AuthSvc, tenant.ContextTxManager{}))
			merchantScoped.GET("/users",
				middleware.RequirePermission("merchant_users:read"),
				userHandler.ListUsers,
			)
			merchantScoped.POST("/users",
				middleware.RequirePermission("merchant_users:write"),
				requireStepUp("merchant.users.add"),
				userHandler.AddUser,
			)
			merchantScoped.DELETE("/users/:userId",
				middleware.RequirePermission("merchant_users:write"),
				requireStepUp("merchant.users.remove"),
				userHandler.RemoveUser,
			)
			merchantScoped.PATCH("/users/:userId/role",
				middleware.RequirePermission("merchant_users:write"),
				requireStepUp("merchant.users.role"),
				userHandler.UpdateRole,
			)
		}

		// ─── Fee Schedule (per-merchant overrides) ───────────────────────────────────
		feeScheduleRepo := crypto_repo.NewFeeScheduleRepo()
		feeHandler := cryptohandlers.NewFeeScheduleHandler(feeScheduleRepo)
		merchantScoped.GET("/fee-schedule",
			middleware.RequirePermission("merchant_fee_schedule:read"),
			feeHandler.ListByMerchant,
		)
		merchantScoped.PUT("/fee-schedule",
			middleware.RequirePermission("merchant_fee_schedule:write"),
			requireStepUp("merchant.fee_schedule.write"),
			feeHandler.UpsertMerchant,
		)
		merchantScoped.DELETE("/fee-schedule",
			middleware.RequirePermission("merchant_fee_schedule:write"),
			requireStepUp("merchant.fee_schedule.delete"),
			feeHandler.DeleteMerchant,
		)
	}

	// ─── Portal Routes (/portal/v1/) ────────────────────────────────────────
	// B2B portal for merchants. Isolated from ERP via MerchantPortal middleware.
	// Security chain: TenantDB → Auth → MerchantPortal
	if m.cfg.PortalDashboardRepo != nil {
		// Build BalanceCalculator for fiat valuation of crypto balances.
		exRateRepo := registers.NewExchangeRateRepo()
		exRateSvc := exchange_rate.NewService(exRateRepo)
		balanceCalc := cryptoapp.NewValuationService(m.cfg.PortalDashboardRepo, exRateSvc, registers.RateSnapshot)

		// Posting engine for debit-first withdrawal requests.
		// Only needs CryptoMerchantBalance visitor+recorder (balance check + movement recording).
		portalDocLocker := postgres.NewDocLocker()
		portalMerchantSvc := crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo())
		portalPostingEngine := posting.NewLockingEngine(portalDocLocker)
		portalPostingEngine.AddVisitor(&cryptoposting.CryptoMerchantBalanceVisitor{})
		portalPostingEngine.AddRecorder(cryptoposting.NewCryptoMerchantBalanceRecorder(portalMerchantSvc))

		if m.cfg.MerchantInvoiceAppSvc == nil {
			panic("merchant portal requires cryptohttp.Config.MerchantInvoiceAppSvc")
		}
		portalHandler := cryptohandlers.NewPortalHandler(m.cfg.PortalDashboardRepo, m.cfg.MerchantAPIKeyRepo, balanceCalc, m.cfg.MerchantInvoiceAppSvc, m.cfg.CheckoutRefs, m.cfg.PaymentLinkSvc, cryptohandlers.PortalCommandServices{
			Withdrawals:     cryptoapp.NewPortalWithdrawalService(m.cfg.PortalDashboardRepo, cfg.Numerator, portalPostingEngine, portalMerchantSvc, cryptoapp.NewMoneyMovementApprovalService(moneyMovementApprovals, m.cfg.MoneyMovementApprovalSigners, nil), crypto_repo.NewChainAddressPolicy()),
			AMLBalances:     cryptoapp.NewAMLBalanceService(crypto_repo.NewAMLRepository()),
			DepositRotation: cryptoapp.NewDepositAddressRotationService(crypto_catalog_repo.NewWalletRepo(), crypto_repo.NewLifecycleWebhookOutbox()),
			Settings:        portalapp.NewSettingsService(m.cfg.PortalDashboardRepo, m.cfg.MerchantInvoiceAppSvc, tenant.ContextTxManager{}),
		}, networkPolicyRepo, merchantCryptoQueries, walletSvc)

		portalV1 := rc.Authenticated("/portal/v1")
		portalV1.Use(cryptomiddleware.MerchantPortal())
		portalV1.Use(cryptomiddleware.MerchantNetworkAccess(networkPolicyRepo, merchant.NetworkSurfacePortal))
		portalV1.Use(middleware.CommandIdempotency(true))
		{
			portalV1.GET("/merchants", portalHandler.ListMerchants)

			dashboard := portalV1.Group("/dashboard")
			{
				dashboard.GET("/summary", portalHandler.GetSummary)
				dashboard.GET("/currencies", portalHandler.GetCurrencies)
				dashboard.GET("/chart", portalHandler.GetChart)
				dashboard.GET("/funnel", portalHandler.GetFunnel)
				dashboard.GET("/balance", portalHandler.GetBalance)
			}

			// ── Balance (dedicated page) ─────────────────────────────────
			portalV1.GET("/balances", portalHandler.GetBalanceDetailed)
			portalV1.GET("/balances/risk", portalHandler.GetRiskBalance)

			portalV1.GET("/invoices", portalHandler.ListInvoices)
			portalV1.GET("/invoices/export", portalHandler.ExportInvoicesCSV)
			portalV1.POST("/invoices",
				cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager),
				portalHandler.CreateInvoice,
			)
			portalV1.GET("/invoices/:id", portalHandler.GetInvoiceDetail) // MUST be after /invoices/export (Gin static-before-param rule)
			portalV1.GET("/payments/:id/refund", portalHandler.GetRefundablePayment)

			// ── Withdrawals ──────────────────────────────────────────────
			portalV1.GET("/withdrawals", portalHandler.ListWithdrawals)

			// ── Withdrawal Addresses (whitelist) ─────────────────────────
			addrs := portalV1.Group("/withdrawal-addresses")
			{
				addrs.GET("", portalHandler.ListWhitelistedAddresses)
				addrs.POST("",
					cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner),
					requireStepUp("portal.withdrawal_addresses.add"),
					portalHandler.AddWhitelistedAddress,
				)
				addrs.DELETE("/:id",
					cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner),
					requireStepUp("portal.withdrawal_addresses.remove"),
					portalHandler.RemoveWhitelistedAddress,
				)
			}

			wreqs := portalV1.Group("/withdrawal-requests")
			{
				wreqs.GET("", portalHandler.ListWithdrawalRequests)
				wreqs.POST("",
					cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager),
					requireStepUp("portal.withdrawal_requests.create"),
					portalHandler.CreateWithdrawalRequest,
				)
				wreqs.POST("/:id/reject",
					cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager),
					requireStepUp("portal.withdrawal_requests.reject"),
					portalHandler.RejectWithdrawalRequest,
				)
			}

			// ── Webhooks ─────────────────────────────────────────────────
			if m.cfg.MerchantWebhookSecretStore != nil {
				registryRepo := crypto_repo.NewMerchantWebhookRepo()
				merchantWebhookHandler := cryptohandlers.NewMerchantWebhookHandler(registryRepo, m.cfg.MerchantWebhookSecretStore, crypto_repo.NewWebhookDeliveryRepo())
				registryHandler := cryptohandlers.NewPortalWebhookRegistryHandler(merchantWebhookHandler)
				endpoints := portalV1.Group("/webhook-endpoints")
				{
					endpoints.GET("", cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager), registryHandler.ListEndpoints)
					endpoints.POST("", cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.create"), registryHandler.CreateEndpoint)
					endpoints.GET("/:id", cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager), registryHandler.GetEndpoint)
					endpoints.PATCH("/:id", cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.update"), registryHandler.UpdateEndpoint)
					endpoints.DELETE("/:id", cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.delete"), registryHandler.DeleteEndpoint)
					endpoints.POST("/:id/rotate-secret", cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.rotate"), registryHandler.RotateSecret)
					endpoints.POST("/:id/test", cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.test"), registryHandler.TestEndpoint)
				}
				events := portalV1.Group("/events")
				{
					events.GET("", cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager), registryHandler.ListEvents)
					events.GET("/:id/deliveries", cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager), registryHandler.ListEventDeliveries)
					events.POST("/:id/redeliver", cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_events.redeliver"), registryHandler.RedeliverEvent)
				}
			}

			// ── API Keys (self-service) ──────────────────────────────────
			// Only registered when MerchantAPIKeyRepo is configured.
			// Without this guard, nil apiKeyRepo causes runtime panic.
			if m.cfg.MerchantAPIKeyRepo != nil {
				keys := portalV1.Group("/api-keys")
				{
					keys.Use(cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner))
					keys.GET("", portalHandler.ListAPIKeys)
					keys.POST("", requireStepUp("portal.api_keys.create"), portalHandler.CreateAPIKey)
					keys.DELETE("/:keyId", requireStepUp("portal.api_keys.delete"), portalHandler.RevokeAPIKey)
					policyHandler := cryptohandlers.NewMerchantNetworkPolicyHandler(networkPolicyRepo, cfg.MFARepo, false)
					keys.GET("/:keyId/network-policy", policyHandler.Get)
					keys.PUT("/:keyId/network-policy", policyHandler.SetMode)
					keys.POST("/:keyId/allowed-networks", policyHandler.AddNetwork)
					keys.DELETE("/:keyId/allowed-networks/:networkId", policyHandler.RevokeNetwork)
				}
			}

			policyHandler := cryptohandlers.NewMerchantNetworkPolicyHandler(networkPolicyRepo, cfg.MFARepo, false)
			policies := portalV1.Group("/security/network-policies")
			policies.Use(cryptomiddleware.RequirePortalRole(merchant.PortalRoleOwner))
			{
				policies.GET("/:surface", policyHandler.Get)
				policies.PUT("/:surface", policyHandler.SetMode)
				policies.POST("/:surface/allowed-networks", policyHandler.AddNetwork)
				policies.DELETE("/:surface/allowed-networks/:networkId", policyHandler.RevokeNetwork)
			}

			// ── Payment Links ────────────────────────────────────────────
			links := portalV1.Group("/payment-links")
			links.Use(cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager))
			{
				links.GET("", portalHandler.ListPaymentLinks)
				links.POST("", portalHandler.CreatePaymentLink)
			}

			// ── Deposit Addresses (persistent customer wallets) ──────────
			depositAddrs := portalV1.Group("/deposit-addresses")
			{
				depositAddrs.GET("", portalHandler.ListDepositAddresses)
				depositAddrs.POST("/:id/rotate", cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager), portalHandler.RotateDepositAddress)
				depositAddrs.POST("",
					cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager),
					portalHandler.CreateDepositAddress,
				)
			}

			// ── Merchant Settings ────────────────────────────────────────
			portalV1.GET("/settings", portalHandler.GetSettings)
			portalV1.PATCH("/settings",
				cryptomiddleware.RequirePortalRole(merchant.PortalRoleManager),
				requireStepUp("portal.settings.update"),
				portalHandler.UpdateSettings,
			)

			// Fee schedule — read-only, any authenticated portal user
			portalV1.GET("/settings/fees", portalHandler.GetFeeSchedule)
		}
	}

	// System-admin recovery is intentionally outside merchant portal policy
	// enforcement, but remains behind platform admin CIDR, MFA, RBAC and a fresh
	// object-bound step-up in the handler.
	recovery := rc.ProtectedGroup("/api/v1/catalog/merchants/:id/network-policies")
	recovery.Use(middleware.RequireSystemAdmin())
	recovery.Use(middleware.CommandIdempotency(true))
	recoveryHandler := cryptohandlers.NewMerchantNetworkPolicyHandler(networkPolicyRepo, cfg.MFARepo, true)
	recovery.GET("/:surface", recoveryHandler.Get)
	recovery.PUT("/:surface", recoveryHandler.SetMode)
	recovery.POST("/:surface/allowed-networks", recoveryHandler.AddNetwork)
	recovery.DELETE("/:surface/allowed-networks/:networkId", recoveryHandler.RevokeNetwork)
}
