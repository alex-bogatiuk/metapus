package cryptohttp

import (
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/command"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/application/dashboardapp"
	"metapus/internal/application/exchangerateapp"
	"metapus/internal/application/integrationapp"
	"metapus/internal/application/portalapp"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_fee"
	"metapus/internal/domain/registers/crypto_merchant_balance"
	"metapus/internal/domain/registers/crypto_receipt_suspense"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/domain/registers/exchange_rate"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/dashboard_repo"
	"metapus/internal/infrastructure/storage/postgres/money_movement_repo"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

func (m *Module) registerWalletPoolRoutes(rg *gin.RouterGroup, cfg v1.RouterConfig) {
	m.registerUnidentifiedReceiptRoutes(rg, cfg)
	repo := crypto_repo.NewWalletPoolRepo()
	svc := crypto.NewWalletPoolAdminService(repo, m.cfg.ChainCapabilities, cfg.EventLogRepo)
	handler := handlers.NewWalletPoolHandler(handlers.NewBaseHandler(), svc)
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
	engine := posting.NewEngine(postgres.NewDocLocker())
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
	handlers.NewUnidentifiedReceiptHandler(handlers.NewBaseHandler(), journal, commands, fundings).
		RegisterRoutes(rg, middleware.SensitiveActionGuard(cfg.MFARepo, "crypto.receipt.credit", cfg.EventLogRepo))
}

func (m *Module) registerCryptoNetworkRoutes(rg *gin.RouterGroup, cfg v1.RouterConfig) {
	handlers.NewCryptoRPCConfigurationHandler(integrationapp.NewRPCConfigurationService(catalog_repo.NewRPCConfigurationRepo(os.Getenv("APP_ENV"), cfg.PolicyEngine), tenant.ContextTxManager{}, cfg.EventLogRepo)).RegisterRoutes(rg, middleware.SensitiveActionGuard(cfg.MFARepo, "system.integrations.write", cfg.EventLogRepo))
	dashboardSvc := dashboardapp.NewService(dashboard_repo.New(), exchange_rate.NewService(register_repo.NewExchangeRateRepo()), register_repo.RateSnapshot)
	handlers.NewAdminDashboardHandler(dashboardSvc).RegisterRoutes(rg)
	repo := crypto_repo.NewNetworkPolicyRepo()
	svc := crypto.NewNetworkPolicyAdminService(repo, m.cfg.ChainCapabilities, cfg.EventLogRepo)
	shadow := crypto.NewShadowControlAdminService(crypto_repo.NewShadowControlRepo(), cfg.EventLogRepo)
	handler := handlers.NewCryptoNetworkHandler(handlers.NewBaseHandler(), svc, shadow)
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
func (m *Module) registerMerchantPublicRoutes(router *gin.Engine, cfg v1.RouterConfig) {
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
	invoiceHandler := handlers.NewMerchantInvoiceHandler(
		m.cfg.MerchantInvoiceSvc, m.cfg.MerchantInvoiceAppSvc, cfg.CheckoutRefs,
		webhookRepo, cryptoapp.NewMerchantCryptoQueryService(crypto_repo.NewMerchantCryptoQueryRepo()),
	)
	apiKeyHandler := handlers.NewMerchantAPIKeyHandler(m.cfg.MerchantAPIKeyRepo)

	walletRepo := catalog_repo.NewWalletRepo()
	walletSvc := wallet.NewService(walletRepo, numerator.Noop())
	merchantCryptoQueries := cryptoapp.NewMerchantCryptoQueryService(crypto_repo.NewMerchantCryptoQueryRepo())
	addressHandler := handlers.NewMerchantAddressHandler(walletSvc, merchantCryptoQueries)

	// /merchant/v1/ — public merchant API (API-key auth)
	merchantBase := router.Group("/merchant/v1")
	merchantBase.Use(middleware.MerchantAPIKey(m.cfg.MerchantAPIKeyRepo, cfg.TenantManager, cfg.MetaPool))
	merchantBase.Use(middleware.MerchantNetworkAccess(networkPolicyRepo, merchant.NetworkSurfaceAPI))
	merchantBase.PATCH("/invoices/:id/reference",
		middleware.RequireMerchantScope(merchant.ScopeInvoiceCreate), invoiceHandler.AuthorizeInvoiceReference,
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
			webhookHandler := handlers.NewMerchantWebhookHandler(webhookRepo, m.cfg.MerchantWebhookSecretStore, crypto_repo.NewWebhookDeliveryRepo())
			endpoints := merchantV1.Group("/webhook-endpoints")
			{
				endpoints.POST("", middleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.CreateEndpoint)
				endpoints.GET("", middleware.RequireMerchantScope(merchant.ScopeWebhookEndpointRead), webhookHandler.ListEndpoints)
				endpoints.GET("/:id", middleware.RequireMerchantScope(merchant.ScopeWebhookEndpointRead), webhookHandler.GetEndpoint)
				endpoints.PATCH("/:id", middleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.UpdateEndpoint)
				endpoints.DELETE("/:id", middleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.DeleteEndpoint)
				endpoints.POST("/:id/rotate-secret", middleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.RotateSecret)
				endpoints.POST("/:id/test", middleware.RequireMerchantScope(merchant.ScopeWebhookEndpointWrite), webhookHandler.TestEndpoint)
			}
			events := merchantV1.Group("/events")
			{
				events.GET("", middleware.RequireMerchantScope(merchant.ScopeWebhookEventRead), webhookHandler.ListEvents)
				events.GET("/:id/deliveries", middleware.RequireMerchantScope(merchant.ScopeWebhookEventRead), webhookHandler.ListEventDeliveries)
				events.POST("/:id/redeliver", middleware.RequireMerchantScope(merchant.ScopeWebhookEventRedeliver), webhookHandler.RedeliverEvent)
			}
		}
	}

	// /api/v1/merchant-admin/ — admin API for managing merchant API keys and users (JWT auth).
	// Security chain matches the main protected group:
	//   TenantDB → Auth → SecurityContext → RequireMerchantAccess → RequirePermission(per route)
	// CWE-639 fix: RequireMerchantAccess validates that the authenticated user
	// has a Manager+ association with the target :merchantId. Global admins bypass.
	merchantAdmin := router.Group("/api/v1/merchant-admin")
	merchantAdmin.Use(middleware.TenantDB(cfg.TenantManager))
	merchantAdmin.Use(middleware.Auth(cfg.JWTValidator))
	merchantAdmin.Use(middleware.AdminGuard(cfg.AdminSecurityMode, cfg.AdminAllowedCIDRs, cfg.AdminMFAMode, cfg.MFARepo, cfg.EventLogRepo))
	merchantAdmin.Use(middleware.SecurityContext(cfg.ProfileProvider))
	merchantAdmin.Use(middleware.CommandIdempotency(true))
	requireStepUp := func(actionCode string) gin.HandlerFunc {
		return middleware.SensitiveActionGuard(cfg.MFARepo, actionCode, cfg.EventLogRepo)
	}
	{
		// Sub-group with ownership check: all routes under /merchants/:merchantId
		// require the user to be associated with that merchant (Owner or Manager).
		merchantScoped := merchantAdmin.Group("/merchants/:merchantId")
		if m.cfg.MerchantUserRepo != nil {
			merchantScoped.Use(middleware.RequireMerchantAccess(m.cfg.MerchantUserRepo, merchant.MerchantRoleManager))
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
			userHandler := handlers.NewMerchantUserHandler(portalapp.NewMerchantUserService(m.cfg.MerchantUserRepo, cfg.AuthSvc, tenant.ContextTxManager{}))
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
		feeHandler := handlers.NewFeeScheduleHandler(feeScheduleRepo)
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
		exRateRepo := register_repo.NewExchangeRateRepo()
		exRateSvc := exchange_rate.NewService(exRateRepo)
		balanceCalc := exchangerateapp.NewValuationService(m.cfg.PortalDashboardRepo, exRateSvc, register_repo.RateSnapshot)

		// Posting engine for debit-first withdrawal requests.
		// Only needs CryptoMerchantBalance visitor+recorder (balance check + movement recording).
		portalDocLocker := postgres.NewDocLocker()
		portalMerchantSvc := crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo())
		portalPostingEngine := posting.NewEngine(portalDocLocker)
		portalPostingEngine.AddVisitor(&cryptoposting.CryptoMerchantBalanceVisitor{})
		portalPostingEngine.AddRecorder(cryptoposting.NewCryptoMerchantBalanceRecorder(portalMerchantSvc))

		if m.cfg.MerchantInvoiceAppSvc == nil {
			panic("merchant portal requires cryptohttp.Config.MerchantInvoiceAppSvc")
		}
		portalHandler := handlers.NewPortalHandler(m.cfg.PortalDashboardRepo, m.cfg.MerchantAPIKeyRepo, balanceCalc, m.cfg.MerchantInvoiceAppSvc, cfg.CheckoutRefs, m.cfg.PaymentLinkSvc, handlers.PortalCommandServices{
			Withdrawals:     cryptoapp.NewPortalWithdrawalService(m.cfg.PortalDashboardRepo, cfg.Numerator, portalPostingEngine, portalMerchantSvc, cryptoapp.NewMoneyMovementApprovalService(moneyMovementApprovals, cfg.MoneyMovementApprovalSigners, nil), crypto_repo.NewChainAddressPolicy()),
			AMLBalances:     cryptoapp.NewAMLBalanceService(crypto_repo.NewAMLRepository()),
			DepositRotation: cryptoapp.NewDepositAddressRotationService(catalog_repo.NewWalletRepo(), crypto_repo.NewLifecycleWebhookOutbox()),
			Settings:        portalapp.NewSettingsService(m.cfg.PortalDashboardRepo, m.cfg.MerchantInvoiceAppSvc, tenant.ContextTxManager{}),
		}, networkPolicyRepo, merchantCryptoQueries, walletSvc)

		portalV1 := router.Group("/portal/v1")
		portalV1.Use(middleware.TenantDB(cfg.TenantManager))
		portalV1.Use(middleware.Auth(cfg.JWTValidator))
		portalV1.Use(middleware.AdminGuard(cfg.AdminSecurityMode, cfg.AdminAllowedCIDRs, cfg.AdminMFAMode, cfg.MFARepo, cfg.EventLogRepo))
		portalV1.Use(middleware.MerchantPortal())
		portalV1.Use(middleware.MerchantNetworkAccess(networkPolicyRepo, merchant.NetworkSurfacePortal))
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
				middleware.RequirePortalRole(merchant.PortalRoleManager),
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
					middleware.RequirePortalRole(merchant.PortalRoleOwner),
					requireStepUp("portal.withdrawal_addresses.add"),
					portalHandler.AddWhitelistedAddress,
				)
				addrs.DELETE("/:id",
					middleware.RequirePortalRole(merchant.PortalRoleOwner),
					requireStepUp("portal.withdrawal_addresses.remove"),
					portalHandler.RemoveWhitelistedAddress,
				)
			}

			wreqs := portalV1.Group("/withdrawal-requests")
			{
				wreqs.GET("", portalHandler.ListWithdrawalRequests)
				wreqs.POST("",
					middleware.RequirePortalRole(merchant.PortalRoleManager),
					requireStepUp("portal.withdrawal_requests.create"),
					portalHandler.CreateWithdrawalRequest,
				)
				wreqs.POST("/:id/reject",
					middleware.RequirePortalRole(merchant.PortalRoleManager),
					requireStepUp("portal.withdrawal_requests.reject"),
					portalHandler.RejectWithdrawalRequest,
				)
			}

			// ── Webhooks ─────────────────────────────────────────────────
			if m.cfg.MerchantWebhookSecretStore != nil {
				registryRepo := crypto_repo.NewMerchantWebhookRepo()
				merchantWebhookHandler := handlers.NewMerchantWebhookHandler(registryRepo, m.cfg.MerchantWebhookSecretStore, crypto_repo.NewWebhookDeliveryRepo())
				registryHandler := handlers.NewPortalWebhookRegistryHandler(merchantWebhookHandler)
				endpoints := portalV1.Group("/webhook-endpoints")
				{
					endpoints.GET("", middleware.RequirePortalRole(merchant.PortalRoleManager), registryHandler.ListEndpoints)
					endpoints.POST("", middleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.create"), registryHandler.CreateEndpoint)
					endpoints.GET("/:id", middleware.RequirePortalRole(merchant.PortalRoleManager), registryHandler.GetEndpoint)
					endpoints.PATCH("/:id", middleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.update"), registryHandler.UpdateEndpoint)
					endpoints.DELETE("/:id", middleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.delete"), registryHandler.DeleteEndpoint)
					endpoints.POST("/:id/rotate-secret", middleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.rotate"), registryHandler.RotateSecret)
					endpoints.POST("/:id/test", middleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_endpoints.test"), registryHandler.TestEndpoint)
				}
				events := portalV1.Group("/events")
				{
					events.GET("", middleware.RequirePortalRole(merchant.PortalRoleManager), registryHandler.ListEvents)
					events.GET("/:id/deliveries", middleware.RequirePortalRole(merchant.PortalRoleManager), registryHandler.ListEventDeliveries)
					events.POST("/:id/redeliver", middleware.RequirePortalRole(merchant.PortalRoleOwner), requireStepUp("portal.webhook_events.redeliver"), registryHandler.RedeliverEvent)
				}
			}

			// ── API Keys (self-service) ──────────────────────────────────
			// Only registered when MerchantAPIKeyRepo is configured.
			// Without this guard, nil apiKeyRepo causes runtime panic.
			if m.cfg.MerchantAPIKeyRepo != nil {
				keys := portalV1.Group("/api-keys")
				{
					keys.Use(middleware.RequirePortalRole(merchant.PortalRoleOwner))
					keys.GET("", portalHandler.ListAPIKeys)
					keys.POST("", requireStepUp("portal.api_keys.create"), portalHandler.CreateAPIKey)
					keys.DELETE("/:keyId", requireStepUp("portal.api_keys.delete"), portalHandler.RevokeAPIKey)
					policyHandler := handlers.NewMerchantNetworkPolicyHandler(networkPolicyRepo, cfg.MFARepo, false)
					keys.GET("/:keyId/network-policy", policyHandler.Get)
					keys.PUT("/:keyId/network-policy", policyHandler.SetMode)
					keys.POST("/:keyId/allowed-networks", policyHandler.AddNetwork)
					keys.DELETE("/:keyId/allowed-networks/:networkId", policyHandler.RevokeNetwork)
				}
			}

			policyHandler := handlers.NewMerchantNetworkPolicyHandler(networkPolicyRepo, cfg.MFARepo, false)
			policies := portalV1.Group("/security/network-policies")
			policies.Use(middleware.RequirePortalRole(merchant.PortalRoleOwner))
			{
				policies.GET("/:surface", policyHandler.Get)
				policies.PUT("/:surface", policyHandler.SetMode)
				policies.POST("/:surface/allowed-networks", policyHandler.AddNetwork)
				policies.DELETE("/:surface/allowed-networks/:networkId", policyHandler.RevokeNetwork)
			}

			// ── Payment Links ────────────────────────────────────────────
			links := portalV1.Group("/payment-links")
			links.Use(middleware.RequirePortalRole(merchant.PortalRoleManager))
			{
				links.GET("", portalHandler.ListPaymentLinks)
				links.POST("", portalHandler.CreatePaymentLink)
			}

			// ── Deposit Addresses (persistent customer wallets) ──────────
			depositAddrs := portalV1.Group("/deposit-addresses")
			{
				depositAddrs.GET("", portalHandler.ListDepositAddresses)
				depositAddrs.POST("/:id/rotate", middleware.RequirePortalRole(merchant.PortalRoleManager), portalHandler.RotateDepositAddress)
				depositAddrs.POST("",
					middleware.RequirePortalRole(merchant.PortalRoleManager),
					portalHandler.CreateDepositAddress,
				)
			}

			// ── Merchant Settings ────────────────────────────────────────
			portalV1.GET("/settings", portalHandler.GetSettings)
			portalV1.PATCH("/settings",
				middleware.RequirePortalRole(merchant.PortalRoleManager),
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
	recovery := router.Group("/api/v1/catalog/merchants/:id/network-policies")
	recovery.Use(middleware.TenantDB(cfg.TenantManager))
	recovery.Use(middleware.Auth(cfg.JWTValidator))
	recovery.Use(middleware.AdminGuard(cfg.AdminSecurityMode, cfg.AdminAllowedCIDRs, cfg.AdminMFAMode, cfg.MFARepo, cfg.EventLogRepo))
	recovery.Use(middleware.SecurityContext(cfg.ProfileProvider))
	recovery.Use(middleware.RequireSystemAdmin())
	recovery.Use(middleware.CommandIdempotency(true))
	recoveryHandler := handlers.NewMerchantNetworkPolicyHandler(networkPolicyRepo, cfg.MFARepo, true)
	recovery.GET("/:surface", recoveryHandler.Get)
	recovery.PUT("/:surface", recoveryHandler.SetMode)
	recovery.POST("/:surface/allowed-networks", recoveryHandler.AddNetwork)
	recovery.DELETE("/:surface/allowed-networks/:networkId", recoveryHandler.RevokeNetwork)
}
