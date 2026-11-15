package cryptohandlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/checkout"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/application/exchangerateapp"
	"metapus/internal/application/portalapp"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
)

type portalPersistentWalletService interface {
	FindPersistentByCustomerID(ctx context.Context, merchantID, networkID id.ID, customerID string) (*wallet.Wallet, error)
	AssignPersistentAddress(ctx context.Context, merchantID, networkID id.ID, customerID string) (*wallet.Wallet, error)
}

// PortalHandler handles portal dashboard endpoints.
// Thin adapter: parse params → call repo/service → return DTO.
type PortalHandler struct {
	repo                   *portal_repo.DashboardRepo
	apiKeyRepo             merchant.APIKeyRepository
	balanceCalc            *exchangerateapp.ValuationService // nil-safe: returns empty balance if not configured
	merchantInvoiceService *cryptoapp.MerchantInvoiceService // shared idempotent invoice creation
	checkoutRefs           *checkout.Service                 // builds customer-facing checkout URLs
	paymentLinkService     *cryptoapp.PaymentLinkService
	withdrawalService      *cryptoapp.PortalWithdrawalService
	amlBalanceService      *cryptoapp.AMLBalanceService
	networkPolicyRepo      merchant.NetworkPolicyRepository
	merchantCryptoQueries  *cryptoapp.MerchantCryptoQueryService
	walletService          portalPersistentWalletService
	depositRotationService *cryptoapp.DepositAddressRotationService
	settingsService        *portalapp.SettingsService
}

type PortalCommandServices struct {
	Withdrawals     *cryptoapp.PortalWithdrawalService
	AMLBalances     *cryptoapp.AMLBalanceService
	DepositRotation *cryptoapp.DepositAddressRotationService
	Settings        *portalapp.SettingsService
}

// NewPortalHandler creates a new portal handler.
func NewPortalHandler(
	repo *portal_repo.DashboardRepo,
	apiKeyRepo merchant.APIKeyRepository,
	balanceCalc *exchangerateapp.ValuationService,
	merchantInvoiceService *cryptoapp.MerchantInvoiceService,
	checkoutRefs *checkout.Service,
	paymentLinkService *cryptoapp.PaymentLinkService,
	services PortalCommandServices,
	networkPolicyRepo merchant.NetworkPolicyRepository,
	merchantCryptoQueries *cryptoapp.MerchantCryptoQueryService,
	walletService portalPersistentWalletService,
) *PortalHandler {
	return &PortalHandler{
		repo:                   repo,
		apiKeyRepo:             apiKeyRepo,
		balanceCalc:            balanceCalc,
		merchantInvoiceService: merchantInvoiceService,
		checkoutRefs:           checkoutRefs,
		paymentLinkService:     paymentLinkService,
		withdrawalService:      services.Withdrawals,
		amlBalanceService:      services.AMLBalances,
		networkPolicyRepo:      networkPolicyRepo,
		merchantCryptoQueries:  merchantCryptoQueries,
		walletService:          walletService,
		depositRotationService: services.DepositRotation,
		settingsService:        services.Settings,
	}
}

// parseActiveMerchant reads optional merchant_id query param, validates it belongs to scope.
func parseActiveMerchant(c *gin.Context) (*id.ID, error) {
	raw := c.Query("merchant_id")
	if raw == "" {
		return nil, nil
	}
	parsed, err := id.Parse(raw)
	if err != nil {
		return nil, apperror.NewValidation("invalid merchant_id")
	}

	// Verify merchant_id is in scope
	scope, ok := merchant.GetPortalScope(c.Request.Context())
	if !ok {
		return nil, apperror.NewForbidden("merchant scope not available")
	}
	if slices.Contains(scope.MerchantIDs, parsed) {
		return &parsed, nil
	}
	return nil, apperror.NewForbidden("merchant_id not in scope")
}

// parseRequiredMerchant reads merchant_id query param (required for write operations).
func parseRequiredMerchant(c *gin.Context) (id.ID, error) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		return id.ID{}, err
	}
	if mid == nil {
		return id.ID{}, apperror.NewValidation("merchant_id query parameter is required")
	}
	return *mid, nil
}

// ── Dashboard ──────────────────────────────────────────────────────────────

// ListMerchants handles GET /portal/v1/merchants
func (h *PortalHandler) ListMerchants(c *gin.Context) {
	items, err := h.repo.ListMerchants(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	clientIP, parseErr := netip.ParseAddr(c.ClientIP())
	if parseErr != nil {
		_ = c.Error(apperror.NewValidation("invalid client IP"))
		c.Abort()
		return
	}
	clientIP = clientIP.Unmap()
	for index := range items {
		items[index].NetworkAccess = string(merchant.NetworkAccessDeny)
		merchantID, parseErr := id.Parse(items[index].ID)
		if parseErr != nil || h.networkPolicyRepo == nil {
			continue
		}
		policies, policyErr := h.networkPolicyRepo.GetEffectiveNetworkPolicies(c.Request.Context(), merchantID, merchant.NetworkSurfacePortal, nil)
		if policyErr != nil {
			continue
		}
		items[index].NetworkAccess = string(merchant.EvaluateNetworkPolicies(policies, clientIP))
	}
	c.JSON(http.StatusOK, dto.PortalMerchantListResponse{Items: items})
}

// GetSummary handles GET /portal/v1/dashboard/summary
func (h *PortalHandler) GetSummary(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	summary, err := h.repo.GetSummary(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, summary)
}

// GetCurrencies handles GET /portal/v1/dashboard/currencies
func (h *PortalHandler) GetCurrencies(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items, err := h.repo.GetCurrencies(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, dto.PortalCurrencyListResponse{Items: items})
}

// GetChart handles GET /portal/v1/dashboard/chart
func (h *PortalHandler) GetChart(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	period := c.DefaultQuery("period", "30d")
	switch period {
	case "7d", "30d", "90d":
		// valid
	default:
		_ = c.Error(apperror.NewValidation("period must be 7d, 30d, or 90d"))
		c.Abort()
		return
	}

	points, err := h.repo.GetChart(c.Request.Context(), period, mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, dto.PortalChartResponse{Items: points})
}

// ListInvoices handles GET /portal/v1/invoices
func (h *PortalHandler) ListInvoices(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// Parse pagination
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Parse filters
	filter := portal_repo.InvoiceFilter{
		Status:   c.Query("status"),
		Search:   c.Query("search"),
		TokenID:  c.Query("token"),
		DateFrom: c.Query("dateFrom"),
		DateTo:   c.Query("dateTo"),
		Sort:     c.DefaultQuery("sort", "created_at"),
		Order:    c.DefaultQuery("order", "desc"),
	}

	// Validate sort column to prevent SQL injection
	switch filter.Sort {
	case "created_at", "number", "status", "amount", "received_amount":
		// valid
	default:
		filter.Sort = "created_at"
	}
	switch filter.Order {
	case "asc", "desc":
		// valid
	default:
		filter.Order = "desc"
	}

	items, total, err := h.repo.ListInvoices(c.Request.Context(), mid, filter, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, dto.PortalInvoiceListResponse{
		Items: items,
		Total: total,
	})
}

// ExportInvoicesCSV handles GET /portal/v1/invoices/export
func (h *PortalHandler) ExportInvoicesCSV(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	filter := portal_repo.InvoiceFilter{
		Status:   c.Query("status"),
		Search:   c.Query("search"),
		TokenID:  c.Query("token"),
		DateFrom: c.Query("dateFrom"),
		DateTo:   c.Query("dateTo"),
		Sort:     c.DefaultQuery("sort", "created_at"),
		Order:    c.DefaultQuery("order", "desc"),
	}

	// Export all matching rows (up to 10000)
	items, _, err := h.repo.ListInvoices(c.Request.Context(), mid, filter, 10000, 0)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Header("Content-Disposition", "attachment; filename=invoices.csv")
	c.Header("Content-Type", "text/csv; charset=utf-8")
	if err := writePortalInvoicesCSV(c.Writer, items); err != nil {
		_ = c.Error(apperror.NewInternal(err))
	}
}

// ── Conversion Funnel ──────────────────────────────────────────────────────

// GetFunnel handles GET /portal/v1/dashboard/funnel
func (h *PortalHandler) GetFunnel(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	funnel, err := h.repo.GetFunnel(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, funnel)
}

// GetBalance handles GET /portal/v1/dashboard/balance?merchant_id=...&source=coingecko
// Returns merchant balance in reporting currency with per-token breakdown.
func (h *PortalHandler) GetBalance(c *gin.Context) {
	h.serveValuedBalance(c)
}

func (h *PortalHandler) serveValuedBalance(c *gin.Context) {
	if _, supplied := c.Request.URL.Query()["source"]; supplied {
		_ = c.Error(apperror.NewInvalidRequest(fmt.Errorf("source is controlled by server settings")))
		c.Abort()
		return
	}
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	if h.balanceCalc == nil {
		_ = c.Error(apperror.NewInternal(fmt.Errorf("valuation service is not configured")))
		c.Abort()
		return
	}
	result, err := h.balanceCalc.Calculate(c.Request.Context(), h.repo.ScopeIDs(c.Request.Context(), mid))
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, result)
}

// ── API Keys (portal self-service) ─────────────────────────────────────────

// ListAPIKeys handles GET /portal/v1/api-keys?merchant_id=...
func (h *PortalHandler) ListAPIKeys(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	keys, err := h.apiKeyRepo.ListByMerchant(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items := make([]dto.MerchantAPIKeyListItem, len(keys))
	for i, k := range keys {
		items[i] = dto.MerchantAPIKeyToListItem(k)
	}
	c.JSON(http.StatusOK, dto.MerchantAPIKeyListResponse{Items: items, Total: len(items)})
}

// CreateAPIKey handles POST /portal/v1/api-keys?merchant_id=...
func (h *PortalHandler) CreateAPIKey(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req dto.CreateMerchantAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	var scopes []merchant.APIKeyScope
	if len(req.Scopes) > 0 {
		scopes = make([]merchant.APIKeyScope, len(req.Scopes))
		for i, s := range req.Scopes {
			scopes[i] = merchant.APIKeyScope(s)
		}
	}

	// Capture creating user from JWT context.
	var createdByUserID *id.ID
	if uc := appctx.GetUser(c.Request.Context()); uc != nil && uc.UserID != "" {
		if uid, parseErr := id.Parse(uc.UserID); parseErr == nil {
			createdByUserID = &uid
		}
	}

	plaintext, key, err := merchant.GenerateKey(mid, req.Name, scopes, req.ExpiresAt, createdByUserID)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}

	if err := key.ValidateAt(c.Request.Context(), time.Now().UTC()); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if err := h.apiKeyRepo.Create(c.Request.Context(), key); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, dto.MerchantAPIKeyToCreateResponse(key, plaintext))
}

// RevokeAPIKey handles DELETE /portal/v1/api-keys/:keyId?merchant_id=...
func (h *PortalHandler) RevokeAPIKey(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	keyID, err := id.Parse(c.Param("keyId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid key id"))
		c.Abort()
		return
	}

	if err := h.apiKeyRepo.Revoke(c.Request.Context(), keyID, mid); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.Status(http.StatusNoContent)
}

// ── Payment Links ──────────────────────────────────────────────────────────

// ListPaymentLinks handles GET /portal/v1/payment-links?merchant_id=...
func (h *PortalHandler) ListPaymentLinks(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	items, total, err := h.repo.ListPaymentLinks(c.Request.Context(), &mid, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	if h.checkoutRefs != nil {
		for i := range items {
			items[i].PayURL = h.checkoutRefs.PaymentLinkPayURL(items[i].PublicRef)
		}
	}
	c.JSON(http.StatusOK, dto.PortalPaymentLinkListResponse{
		Items: items,
		Total: total,
	})
}

// CreatePaymentLink handles POST /portal/v1/payment-links?merchant_id=...
func (h *PortalHandler) CreatePaymentLink(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req dto.CreatePaymentLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	// Parse amount
	amount, err := types.NewCryptoAmountFromString(req.Amount)
	if err != nil || !amount.IsPositive() {
		_ = c.Error(apperror.NewValidation("amount must be a positive integer string"))
		c.Abort()
		return
	}

	// Parse token ID
	tokenID, err := id.Parse(req.TokenID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid tokenId: " + req.TokenID))
		c.Abort()
		return
	}

	// Defaults
	ttl := req.TTLMinutes
	if ttl == 0 {
		settings, settingsErr := h.repo.GetMerchantSettings(c.Request.Context(), mid)
		if settingsErr != nil {
			_ = c.Error(settingsErr)
			c.Abort()
			return
		}
		ttl = settings.DefaultTTLMinutes
	}
	if ttl < 5 || ttl > 10080 {
		_ = c.Error(apperror.NewValidation("ttlMinutes must be between 5 and 10080"))
		c.Abort()
		return
	}

	maxUses := req.MaxUses
	if !req.Reusable {
		maxUses = 1
	}

	var createdBy *id.ID
	if uc := appctx.GetUser(c.Request.Context()); uc != nil && uc.UserID != "" {
		if uid, parseErr := id.Parse(uc.UserID); parseErr == nil {
			createdBy = &uid
		}
	}

	if h.paymentLinkService == nil {
		_ = c.Error(apperror.NewInternal(errors.New("payment link service is not configured")))
		c.Abort()
		return
	}
	result, err := h.paymentLinkService.Create(c.Request.Context(), cryptoapp.CreatePaymentLinkInput{
		MerchantID:  mid,
		TokenID:     tokenID,
		Amount:      amount,
		Description: req.Description,
		Reusable:    req.Reusable,
		MaxUses:     maxUses,
		TTLMinutes:  ttl,
		CreatedBy:   createdBy,
	})
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, dto.PortalPaymentLinkCreateResponse{
		Object:    "payment_link",
		ID:        result.ID.String(),
		PublicRef: result.PublicRef,
		PayURL:    result.PayURL,
	})
}

// ── Merchant Settings ──────────────────────────────────────────────────────

// GetSettings handles GET /portal/v1/settings?merchant_id=...
func (h *PortalHandler) GetSettings(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	settings, err := h.repo.GetMerchantSettings(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, settings)
}

// UpdateSettings handles PATCH /portal/v1/settings?merchant_id=...
func (h *PortalHandler) UpdateSettings(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req dto.UpdatePortalSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	settings, err := h.settingsService.Update(c.Request.Context(), mid, req)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, settings)
}
