// Package cryptohttp mounts the crypto-processing configuration's HTTP
// routes (checkout, merchant API, merchant portal and crypto administration)
// on the platform router through v1.RouteModule.
package cryptohttp

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/http/cryptohttp/cryptohandlers"
	"metapus/internal/infrastructure/http/cryptohttp/cryptomiddleware"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
)

// Config holds the crypto configuration's HTTP dependencies.
type Config struct {
	// ChainCapabilities describes the family adapters linked into this binary.
	ChainCapabilities crypto.ChainFamilyCapabilityResolver

	// MerchantAPIKeyRepo enables the /merchant/v1/ public API.
	// If set, the merchant invoice routes are registered with API-key auth.
	MerchantAPIKeyRepo merchant.APIKeyRepository

	// MerchantInvoiceSvc is the crypto invoice service for checkout and the merchant public API.
	MerchantInvoiceSvc domain.DocumentService[*crypto_invoice.CryptoInvoice]

	// MerchantInvoiceAppSvc owns idempotent invoice creation for the public API and portal.
	// Required when MerchantAPIKeyRepo or PortalDashboardRepo is set.
	MerchantInvoiceAppSvc *cryptoapp.MerchantInvoiceService

	// MerchantWebhookSecretStore persists endpoint secrets outside PostgreSQL.
	// Public webhook management routes are enabled only when this is configured.
	MerchantWebhookSecretStore crypto.WebhookSecretStore

	// PaymentLinkSvc owns portal payment link creation and public refs.
	PaymentLinkSvc *cryptoapp.PaymentLinkService

	// MerchantUserRepo manages user ↔ merchant access associations.
	// Required for merchant user management endpoints.
	MerchantUserRepo merchant.MerchantUserRepository

	// PortalDashboardRepo provides portal dashboard queries (scope-filtered).
	// If set, the /portal/v1/ routes are registered.
	PortalDashboardRepo *portal_repo.DashboardRepo
}

// Module is the crypto configuration's v1.RouteModule.
type Module struct {
	cfg Config
}

// New creates the crypto route module.
func New(cfg Config) *Module {
	return &Module{cfg: cfg}
}

var _ v1.RouteModule = (*Module)(nil)

// RegisterRoutes mounts every crypto route on the platform router.
func (m *Module) RegisterRoutes(rc v1.RouteContext) {
	cfg := rc.Config
	m.registerCheckoutRoutes(rc.Root, rc.API, cfg)
	m.registerWalletPoolRoutes(rc.Protected, cfg)
	m.registerCryptoNetworkRoutes(rc.Protected, cfg)
	m.registerFeeScheduleRoutes(rc.System, cfg)
	if m.cfg.MerchantAPIKeyRepo != nil {
		m.registerMerchantPublicRoutes(rc.Root, cfg)
	}
}

// registerCheckoutRoutes mounts the public payment page and checkout API.
// Tenant is resolved from the opaque public ref before a tenant database
// connection is opened.
func (m *Module) registerCheckoutRoutes(router *gin.Engine, api *gin.RouterGroup, cfg v1.RouterConfig) {
	RegisterPaymentPage(router)

	if cfg.CheckoutRefs == nil {
		panic("cryptohttp: v1.RouterConfig.CheckoutRefs must not be nil")
	}
	checkoutPageService := cryptoapp.NewCheckoutPageService(crypto_repo.NewCheckoutPageRepo(), m.cfg.MerchantInvoiceSvc, crypto_repo.NewMerchantInvoiceSettingsRepo())
	paymentPageHandler := cryptohandlers.NewPaymentPageHandler(checkoutPageService)

	checkoutGroup := api.Group("/checkout")
	checkoutGroup.Use(cryptomiddleware.CheckoutTenantDB(cfg.TenantManager, cfg.CheckoutRefs))
	{
		invoices := checkoutGroup.Group("/invoices")
		{
			invoices.GET("/:publicRef", paymentPageHandler.GetPaymentInfo)
			invoices.GET("/:publicRef/status", paymentPageHandler.GetInvoiceStatus)
		}
		links := checkoutGroup.Group("/links")
		{
			links.POST("/:publicRef/redeem", paymentPageHandler.RedeemPaymentLink)
		}
	}
}

// registerFeeScheduleRoutes mounts the global processing fee defaults.
func (m *Module) registerFeeScheduleRoutes(sysGroup *gin.RouterGroup, cfg v1.RouterConfig) {
	feeScheduleHandler := cryptohandlers.NewFeeScheduleHandler(crypto_repo.NewFeeScheduleRepo())
	requireStepUp := func(actionCode string) gin.HandlerFunc {
		return middleware.SensitiveActionGuard(cfg.MFARepo, actionCode, cfg.EventLogRepo)
	}
	feeGroup := sysGroup.Group("/fee-schedule")
	{
		feeGroup.GET("", feeScheduleHandler.ListGlobal)
		feeGroup.PUT("", requireStepUp("system.fee_schedule.write"), feeScheduleHandler.UpsertGlobal)
		feeGroup.DELETE("", requireStepUp("system.fee_schedule.delete"), feeScheduleHandler.DeleteGlobal)
	}
}
