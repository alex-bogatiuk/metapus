package handlers

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_merchant_balance"
	"metapus/internal/infrastructure/http/v1/dto"
	numerator "metapus/internal/platform"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
)

// _blockchainAddressRe validates blockchain address format: alphanumeric only.
var _blockchainAddressRe = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// _maxInt64Dec is the maximum int64 value as decimal, cached for overflow checks.
var _maxInt64Dec = decimal.NewFromInt(math.MaxInt64)

// PortalHandler handles portal dashboard endpoints.
// Thin adapter: parse params → call repo/service → return DTO.
type PortalHandler struct {
	repo               *portal_repo.DashboardRepo
	apiKeyRepo         merchant.APIKeyRepository
	balanceCalc        *crypto.BalanceCalculator // nil-safe: returns empty balance if not configured
	rateSourceResolver crypto.RateSourceResolver // resolves source code → UUID
	webhookDispatcher  *crypto.WebhookDispatcher // nil-safe: test webhook returns error
	deliveryRepo       crypto.WebhookDeliveryRepository // nil-safe: delivery persistence is best-effort
	invoiceService     domain.DocumentService[*crypto_invoice.CryptoInvoice] // for portal invoice creation
	numeratorService   numerator.Generator // for generating document numbers
	postingEngine      *posting.Engine // for debit-first withdrawal request posting
	merchantBalanceSvc *crypto_merchant_balance.Service // for storno movements on rejection
}

// NewPortalHandler creates a new portal handler.
func NewPortalHandler(
	repo *portal_repo.DashboardRepo,
	apiKeyRepo merchant.APIKeyRepository,
	balanceCalc *crypto.BalanceCalculator,
	rateSourceResolver crypto.RateSourceResolver,
	webhookDispatcher *crypto.WebhookDispatcher,
	deliveryRepo crypto.WebhookDeliveryRepository,
	invoiceService domain.DocumentService[*crypto_invoice.CryptoInvoice],
	numeratorService numerator.Generator,
	postingEngine *posting.Engine,
	merchantBalanceSvc *crypto_merchant_balance.Service,
) *PortalHandler {
	return &PortalHandler{
		repo:               repo,
		apiKeyRepo:         apiKeyRepo,
		balanceCalc:        balanceCalc,
		rateSourceResolver: rateSourceResolver,
		webhookDispatcher:  webhookDispatcher,
		deliveryRepo:       deliveryRepo,
		invoiceService:     invoiceService,
		numeratorService:   numeratorService,
		postingEngine:      postingEngine,
		merchantBalanceSvc: merchantBalanceSvc,
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
	scope, ok := appctx.GetMerchantScope(c.Request.Context())
	if !ok {
		return nil, apperror.NewForbidden("merchant scope not available")
	}
	for _, sid := range scope.MerchantIDs {
		if sid == parsed {
			return &parsed, nil
		}
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
	c.JSON(http.StatusOK, gin.H{"items": items})
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
	c.JSON(http.StatusOK, gin.H{"items": items})
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
	c.JSON(http.StatusOK, gin.H{"items": points})
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
	// BOM for Excel UTF-8 detection
	_, _ = c.Writer.Write([]byte("\xEF\xBB\xBF"))
	_, _ = c.Writer.WriteString("Number,Status,Token,Network,Amount,Received,TxHash,Fee,Net,Date\n")
	for _, inv := range items {
		_, _ = c.Writer.WriteString(
			inv.Number + "," +
				inv.Status + "," +
				inv.Symbol + "," +
				inv.Network + "," +
				inv.Amount + "," +
				inv.ReceivedAmount + "," +
				inv.TxHash + "," +
				inv.ProcessingFee + "," +
				inv.NetAmount + "," +
				inv.CreatedAt + "\n",
		)
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
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if h.balanceCalc == nil {
		// Balance calculator not configured — return empty balance.
		c.JSON(http.StatusOK, dto.PortalBalanceResponse{
			ByToken: []dto.PortalTokenBalance{},
		})
		return
	}

	ctx := c.Request.Context()
	sourceCode := c.DefaultQuery("source", "coingecko")

	// Resolve source code to UUID.
	rateSourceID, err := h.rateSourceResolver.ResolveRateSourceID(ctx, sourceCode)
	if err != nil {
		_ = c.Error(apperror.NewValidation("unknown rate source: " + sourceCode))
		c.Abort()
		return
	}

	// Resolve merchant IDs from scope (same as other dashboard methods).
	ids := h.repo.ScopeIDs(ctx, mid)

	balance, err := h.balanceCalc.CalculateForMerchants(ctx, h.repo, ids, rateSourceID, nil)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// Map domain → DTO
	byToken := make([]dto.PortalTokenBalance, 0, len(balance.ByToken))
	for _, tb := range balance.ByToken {
		byToken = append(byToken, dto.PortalTokenBalance{
			TokenID:      tb.TokenID.String(),
			TokenSymbol:  tb.TokenSymbol,
			CurrencyCode: tb.CurrencyCode,
			RawAmount:    tb.RawAmount,
			HumanAmount:  tb.HumanAmount.StringFixed(8),
			Rate:         tb.Rate.StringFixed(12),
			Multiplier:   tb.Multiplier,
			BaseAmount:   tb.BaseAmount.StringFixed(2),
			HasRate:      tb.HasRate,
		})
	}

	c.JSON(http.StatusOK, dto.PortalBalanceResponse{
		TotalBase:    balance.TotalBase.StringFixed(2),
		BaseCurrency: balance.BaseCurrency,
		RateSource:   sourceCode,
		ByToken:      byToken,
	})
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
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
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
		_ = c.Error(apperror.NewValidation(err.Error()))
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

	if err := key.Validate(c.Request.Context()); err != nil {
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
	mid, err := parseActiveMerchant(c)
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

	items, total, err := h.repo.ListPaymentLinks(c.Request.Context(), mid, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
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
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	// Parse amount
	amount, ok := new(big.Int).SetString(req.Amount, 10)
	if !ok || amount.Sign() <= 0 {
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
		ttl = 60
	}
	if ttl < 5 || ttl > 1440 {
		_ = c.Error(apperror.NewValidation("ttlMinutes must be between 5 and 1440"))
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

	linkID, shortCode, err := h.repo.CreatePaymentLink(
		c.Request.Context(), mid, tokenID, amount,
		req.Description, req.Reusable, maxUses, ttl, createdBy,
	)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, dto.PortalPaymentLinkCreateResponse{
		ID:        linkID.String(),
		ShortCode: shortCode,
		PayURL:    "/pay/link/" + shortCode,
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
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	if err := h.repo.UpdateMerchantSettings(c.Request.Context(), mid, req); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// Return updated settings
	settings, err := h.repo.GetMerchantSettings(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, settings)
}

// ── Invoice Detail ─────────────────────────────────────────────────────────

// GetInvoiceDetail handles GET /portal/v1/invoices/:id
func (h *PortalHandler) GetInvoiceDetail(c *gin.Context) {
	invoiceID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid invoice id"))
		c.Abort()
		return
	}

	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	detail, err := h.repo.GetInvoiceDetail(c.Request.Context(), invoiceID, mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, detail)
}

// ── Withdrawals ────────────────────────────────────────────────────────────

// ListWithdrawals handles GET /portal/v1/withdrawals
func (h *PortalHandler) ListWithdrawals(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
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

	filter := portal_repo.WithdrawalFilter{
		Status: c.Query("status"),
		Sort:   c.DefaultQuery("sort", "created_at"),
		Order:  c.DefaultQuery("order", "desc"),
	}

	// Validate sort column
	switch filter.Sort {
	case "created_at", "number", "status", "amount":
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

	items, total, err := h.repo.ListWithdrawals(c.Request.Context(), mid, filter, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, dto.PortalWithdrawalListResponse{
		Items: items,
		Total: total,
	})
}

// ── Webhook Deliveries ─────────────────────────────────────────────────────

// ListWebhookDeliveries handles GET /portal/v1/webhooks/deliveries?merchant_id=...
func (h *PortalHandler) ListWebhookDeliveries(c *gin.Context) {
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

	items, total, err := h.repo.GetWebhookDeliveriesByMerchant(c.Request.Context(), mid, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

// SendTestWebhook handles POST /portal/v1/webhooks/test?merchant_id=...
func (h *PortalHandler) SendTestWebhook(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if h.webhookDispatcher == nil {
		_ = c.Error(apperror.NewInternal(fmt.Errorf("webhook dispatcher not configured")))
		c.Abort()
		return
	}

	// Get merchant webhook config
	webhookURL, webhookSecret, err := h.repo.GetMerchantWebhookSecret(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if webhookURL == "" {
		_ = c.Error(apperror.NewValidation("webhook URL not configured for this merchant"))
		c.Abort()
		return
	}

	// Send test payload
	data := map[string]interface{}{
		"message":    "Test webhook from Metapus",
		"merchantId": mid.String(),
	}

	delivery, _ := h.webhookDispatcher.Dispatch(
		c.Request.Context(),
		h.deliveryRepo,
		nil, // no invoice for test
		mid,
		webhookURL,
		webhookSecret,
		crypto.WebhookEventType("test"),
		data,
		1,
	)

	resp := dto.PortalTestWebhookResponse{
		Success: delivery != nil && delivery.IsSuccess(),
	}
	if delivery != nil {
		resp.StatusCode = delivery.StatusCode
		resp.ResponseTimeMs = delivery.ResponseTimeMs
		resp.Error = delivery.ErrorMessage
	}

	c.JSON(http.StatusOK, resp)
}

// ── Webhook Secret Management ──────────────────────────────────────────

// RevealWebhookSecret handles POST /portal/v1/settings/webhook-secret/reveal
// Returns the full webhook signing secret. POST (not GET) because this is an audit-sensitive action.
// Requires Owner role (enforced at router level).
func (h *PortalHandler) RevealWebhookSecret(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	_, secret, err := h.repo.GetMerchantWebhookSecret(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if secret == "" {
		// Auto-generate on first reveal if not yet set
		secret, err = h.repo.RotateWebhookSecret(c.Request.Context(), mid)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
	}

	c.JSON(http.StatusOK, dto.PortalWebhookSecretResponse{Secret: secret})
}

// RotateWebhookSecret handles POST /portal/v1/settings/webhook-secret/rotate
// Generates a new webhook secret. The old secret is immediately invalidated.
// Requires Owner role (enforced at router level).
func (h *PortalHandler) RotateWebhookSecret(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	secret, err := h.repo.RotateWebhookSecret(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.PortalWebhookSecretResponse{Secret: secret})
}

// ── Fee Schedule ───────────────────────────────────────────────────────

// GetFeeSchedule handles GET /portal/v1/settings/fees
// Returns the effective fee schedule for the merchant (merchant-specific overrides + global defaults).
// Shows only "processing" and "withdrawal" directions.
func (h *PortalHandler) GetFeeSchedule(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items, err := h.repo.GetEffectiveFees(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.PortalFeeScheduleResponse{Items: items})
}

// ── Create Invoice ─────────────────────────────────────────────────────

const (
	_portalDefaultTTLMinutes = 60
	_portalMinTTLMinutes     = 5
	_portalMaxTTLMinutes     = 1440 // 24h
)

// CreateInvoice handles POST /portal/v1/invoices
// Thin proxy: validates portal auth → converts human amount → delegates to InvoiceService.Create.
// Requires Manager role.
func (h *PortalHandler) CreateInvoice(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if h.invoiceService == nil {
		_ = c.Error(apperror.NewInternal(fmt.Errorf("invoice service not configured")))
		c.Abort()
		return
	}

	var req dto.PortalCreateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	ctx := c.Request.Context()
	pool := tenant.MustGetPool(ctx)

	// 1. Resolve token and get decimal places
	tokenID, err := id.Parse(req.TokenID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid token ID"))
		c.Abort()
		return
	}

	var decimalPlaces int
	var symbol, network string
	err = pool.QueryRow(ctx, `
		SELECT t.decimal_places, t.symbol, COALESCE(n.name, '') AS network
		FROM cat_tokens t
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE t.id = $1 AND t.deletion_mark = FALSE
	`, tokenID).Scan(&decimalPlaces, &symbol, &network)
	if err != nil {
		_ = c.Error(apperror.NewValidation("unknown token"))
		c.Abort()
		return
	}

	// 2. Convert human-readable amount → minor units
	humanAmount, err := decimal.NewFromString(req.Amount)
	if err != nil || humanAmount.LessThanOrEqual(decimal.Zero) {
		_ = c.Error(apperror.NewValidation("amount must be a positive number"))
		c.Abort()
		return
	}

	multiplier := decimal.New(1, int32(decimalPlaces))
	minorUnitsDecimal := humanAmount.Mul(multiplier)
	if !minorUnitsDecimal.Equal(minorUnitsDecimal.Truncate(0)) {
		_ = c.Error(apperror.NewValidation(
			fmt.Sprintf("amount has too many decimal places (max %d)", decimalPlaces)))
		c.Abort()
		return
	}
	if minorUnitsDecimal.GreaterThan(_maxInt64Dec) {
		_ = c.Error(apperror.NewValidation("amount too large"))
		c.Abort()
		return
	}
	amount := types.NewCryptoAmountFromInt64(minorUnitsDecimal.IntPart())

	// 3. Build domain entity
	inv := crypto_invoice.NewCryptoInvoice(mid, tokenID, amount)
	inv.Status = crypto_invoice.InvoiceStatusCreated

	// TTL
	ttl := _portalDefaultTTLMinutes
	if req.TTLMinutes != nil {
		ttl = *req.TTLMinutes
	}
	if ttl < _portalMinTTLMinutes || ttl > _portalMaxTTLMinutes {
		_ = c.Error(apperror.NewValidation(
			fmt.Sprintf("ttlMinutes must be between %d and %d", _portalMinTTLMinutes, _portalMaxTTLMinutes)))
		c.Abort()
		return
	}
	inv.ExpiresAt = time.Now().Add(time.Duration(ttl) * time.Minute)

	// Optional fields
	if req.OrderID != nil {
		inv.OrderID = *req.OrderID
	}
	if req.Description != nil {
		inv.Description = *req.Description
	}
	if req.CustomerEmail != nil {
		inv.CustomerEmail = *req.CustomerEmail
	}

	// 4. Create via service (triggers hooks: wallet lease, outbox, etc.)
	if err := h.invoiceService.Create(ctx, inv); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// 5. Reload wallet address
	var walletAddress string
	_ = pool.QueryRow(ctx, `
		SELECT COALESCE(w.address, '')
		FROM doc_crypto_invoices ci
		LEFT JOIN cat_wallets w ON w.id = ci.wallet_id
		WHERE ci.id = $1
	`, inv.ID).Scan(&walletAddress)

	c.JSON(http.StatusCreated, dto.PortalCreateInvoiceResponse{
		ID:            inv.ID.String(),
		Number:        inv.Number,
		Status:        string(inv.Status),
		Amount:        inv.ExpectedAmount.String(),
		Symbol:        symbol,
		Network:       network,
		DecimalPlaces: decimalPlaces,
		WalletAddress: walletAddress,
		ExpiresAt:     inv.ExpiresAt.Format(time.RFC3339),
		CreatedAt:     inv.CreatedAt.Format(time.RFC3339),
	})
}

// ── Balance Detailed (three-bucket) ────────────────────────────────────

// GetBalanceDetailed handles GET /portal/v1/balances
// Returns three-bucket balance: available (confirmed), pending (unconfirmed), total.
func (h *PortalHandler) GetBalanceDetailed(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if h.balanceCalc == nil {
		c.JSON(http.StatusOK, dto.PortalDetailedBalanceResponse{
			ByToken: []dto.PortalTokenDetailed{},
		})
		return
	}

	ctx := c.Request.Context()
	sourceCode := c.DefaultQuery("source", "coingecko")

	rateSourceID, err := h.rateSourceResolver.ResolveRateSourceID(ctx, sourceCode)
	if err != nil {
		_ = c.Error(apperror.NewValidation("unknown rate source: " + sourceCode))
		c.Abort()
		return
	}

	ids := h.repo.ScopeIDs(ctx, mid)

	balance, err := h.balanceCalc.CalculateForMerchants(ctx, h.repo, ids, rateSourceID, nil)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// Get pending amounts (invoices in 'paid' status)
	pendingMap, err := h.repo.GetPendingAmounts(ctx, ids)
	if err != nil {
		// Non-fatal: continue with zero pending
		pendingMap = make(map[string]int64)
	}

	var totalPending, totalAvailable big.Int
	byToken := make([]dto.PortalTokenDetailed, 0, len(balance.ByToken))
	for _, tb := range balance.ByToken {
		tokenIDStr := tb.TokenID.String()
		pendingRaw := pendingMap[tokenIDStr]

		// Total raw = register balance (confirmed)
		totalRawInt := tb.HumanAmount // already in human form from calculator
		rawAmountStr := tb.RawAmount

		// For pending: convert to human format
		// We need decimal places — derive from the raw/human ratio
		dp := 0
		if rawAmountStr != "0" && rawAmountStr != "" {
			dp = deriveDPFromRawHuman(rawAmountStr, totalRawInt.String())
		}

		pendingHuman := portal_repo.FormatMinorUnits(pendingRaw, dp)

		// available = total - pending (in minor units)
		totalRawBig, _ := new(big.Int).SetString(rawAmountStr, 10)
		if totalRawBig == nil {
			totalRawBig = new(big.Int)
		}
		pendingBig := big.NewInt(pendingRaw)
		availableBig := new(big.Int).Sub(totalRawBig, pendingBig)
		if availableBig.Sign() < 0 {
			availableBig.SetInt64(0)
		}

		// Guard against int64 overflow for very large balances.
		var availableHuman string
		if availableBig.IsInt64() {
			availableHuman = portal_repo.FormatMinorUnits(availableBig.Int64(), dp)
		} else {
			availableHuman = availableBig.String() // fallback: raw minor units
		}

		// Running totals for base amounts (simplified: proportional)
		totalPending.Add(&totalPending, pendingBig)
		totalAvailable.Add(&totalAvailable, availableBig)

		byToken = append(byToken, dto.PortalTokenDetailed{
			PortalTokenBalance: dto.PortalTokenBalance{
				TokenID:      tb.TokenID.String(),
				TokenSymbol:  tb.TokenSymbol,
				CurrencyCode: tb.CurrencyCode,
				RawAmount:    rawAmountStr,
				HumanAmount:  tb.HumanAmount.StringFixed(8),
				Rate:         tb.Rate.StringFixed(12),
				Multiplier:   tb.Multiplier,
				BaseAmount:   tb.BaseAmount.StringFixed(2),
				HasRate:      tb.HasRate,
			},
			PendingRaw:     strconv.FormatInt(pendingRaw, 10),
			PendingHuman:   pendingHuman,
			AvailableRaw:   availableBig.String(),
			AvailableHuman: availableHuman,
		})
	}

	// Calculate base currency totals (proportional estimate)
	// For a precise calculation we'd need to convert pending per-token to base,
	// but for simplicity we compute: pendingBase = totalBase * pendingPortion
	pendingBaseStr := "0.00"
	availableBaseStr := balance.TotalBase.StringFixed(2)
	// Simple approach: if we have pending tokens, estimate their base value
	// For now, just return total as available (pending base = 0) — this is a UI approximation
	// TODO: Calculate precise pending base using exchange rates per token

	c.JSON(http.StatusOK, dto.PortalDetailedBalanceResponse{
		TotalBase:     balance.TotalBase.StringFixed(2),
		PendingBase:   pendingBaseStr,
		AvailableBase: availableBaseStr,
		BaseCurrency:  balance.BaseCurrency,
		RateSource:    sourceCode,
		ByToken:       byToken,
	})
}

// deriveDPFromRawHuman infers decimal places from raw/human ratio.
// Fallback: returns 6 if unable to determine.
func deriveDPFromRawHuman(raw, human string) int {
	rawBig, ok1 := new(big.Int).SetString(raw, 10)
	humanBig, ok2 := new(big.Float).SetString(human)
	if !ok1 || !ok2 || humanBig.Sign() == 0 {
		return 6
	}
	ratio := new(big.Float).Quo(new(big.Float).SetInt(rawBig), humanBig)
	ratioInt, _ := ratio.Int64()
	dp := 0
	for ratioInt > 1 {
		ratioInt /= 10
		dp++
	}
	return dp
}

// ── Withdrawal Address Whitelist ───────────────────────────────────────

// ListWhitelistedAddresses handles GET /portal/v1/withdrawal-addresses
func (h *PortalHandler) ListWhitelistedAddresses(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items, err := h.repo.ListWhitelistedAddresses(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.PortalWithdrawalAddressListResponse{Items: items})
}

// AddWhitelistedAddress handles POST /portal/v1/withdrawal-addresses (Owner only)
func (h *PortalHandler) AddWhitelistedAddress(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req dto.PortalAddWithdrawalAddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	// Validate blockchain address format: trim, length, alphanumeric.
	req.Address = strings.TrimSpace(req.Address)
	if req.Address == "" {
		_ = c.Error(apperror.NewValidation("address is required"))
		c.Abort()
		return
	}
	if len(req.Address) < 20 || len(req.Address) > 128 {
		_ = c.Error(apperror.NewValidation("address length must be between 20 and 128 characters"))
		c.Abort()
		return
	}
	if !_blockchainAddressRe.MatchString(req.Address) {
		_ = c.Error(apperror.NewValidation("address contains invalid characters"))
		c.Abort()
		return
	}

	newID, err := h.repo.AddWhitelistedAddress(c.Request.Context(), mid, req)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": newID})
}

// RemoveWhitelistedAddress handles DELETE /portal/v1/withdrawal-addresses/:id (Owner only)
func (h *PortalHandler) RemoveWhitelistedAddress(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	addressID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid address id"))
		c.Abort()
		return
	}

	if err := h.repo.RemoveWhitelistedAddress(c.Request.Context(), mid, addressID); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// ── Withdrawal Requests ───────────────────────────────────────────────

// CreateWithdrawalRequest handles POST /portal/v1/withdrawal-requests (Manager+)
func (h *PortalHandler) CreateWithdrawalRequest(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req dto.PortalCreateWithdrawalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	ctx := c.Request.Context()
	pool := tenant.MustGetPool(ctx)

	// 1. Resolve token (need decimal_places for amount conversion + network_id for address validation)
	tokenID, err := id.Parse(req.TokenID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid token ID"))
		c.Abort()
		return
	}

	var decimalPlaces int
	var tokenNetworkID string
	err = pool.QueryRow(ctx,
		`SELECT decimal_places, network_id::text FROM cat_tokens WHERE id = $1 AND deletion_mark = FALSE`,
		tokenID,
	).Scan(&decimalPlaces, &tokenNetworkID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("unknown token"))
		c.Abort()
		return
	}

	// 2. Validate whitelisted address
	addressID, err := id.Parse(req.AddressID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid address ID"))
		c.Abort()
		return
	}

	addr, err := h.repo.GetWhitelistedAddress(ctx, mid, addressID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("address not found in whitelist"))
		c.Abort()
		return
	}

	// Verify address is whitelisted for the same network as the token being withdrawn.
	// Prevents cross-network withdrawal (e.g. TRON address for Ethereum token).
	if addr.NetworkID != tokenNetworkID {
		_ = c.Error(apperror.NewValidation("address is not whitelisted for this token's network"))
		c.Abort()
		return
	}

	// 3. Convert human-readable amount → minor units
	humanAmount, err := decimal.NewFromString(req.Amount)
	if err != nil || humanAmount.LessThanOrEqual(decimal.Zero) {
		_ = c.Error(apperror.NewValidation("amount must be a positive number"))
		c.Abort()
		return
	}

	multiplier := decimal.New(1, int32(decimalPlaces))
	minorUnitsDecimal := humanAmount.Mul(multiplier)
	if !minorUnitsDecimal.Equal(minorUnitsDecimal.Truncate(0)) {
		_ = c.Error(apperror.NewValidation(
			fmt.Sprintf("amount has too many decimal places (max %d)", decimalPlaces)))
		c.Abort()
		return
	}
	if minorUnitsDecimal.GreaterThan(_maxInt64Dec) {
		_ = c.Error(apperror.NewValidation("amount too large"))
		c.Abort()
		return
	}
	amount := minorUnitsDecimal.IntPart()

	// 4. Generate number via numerator (prevents race condition on concurrent requests)
	numCfg := numerator.DefaultNumeratorConfig("WR")
	number, err := h.numeratorService.GetNextNumber(ctx, numCfg, nil, time.Now())
	if err != nil {
		_ = c.Error(fmt.Errorf("generate withdrawal number: %w", err))
		c.Abort()
		return
	}

	// 5. Build domain entity
	cryptoAmount := types.NewCryptoAmountFromInt64(amount)
	wrDoc := withdrawal_request.NewWithdrawalRequest(
		mid, tokenID, addressID,
		addr.Address,
		cryptoAmount,
	)
	wrDoc.Number = number

	// 6. Post (debit-first): Engine.Post() → ValidateBeforePost → FOR UPDATE balance check
	// On success: Expense movement recorded, merchant balance reduced immediately.
	// The updateDoc callback persists the document with posted=true.
	err = h.postingEngine.Post(ctx, wrDoc, func(txCtx context.Context) error {
		return h.repo.CreateWithdrawalRequest(
			txCtx, wrDoc.ID, mid, tokenID, amount, addr.Address, addressID, number,
			wrDoc.Posted, wrDoc.PostedVersion,
		)
	})
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": wrDoc.ID, "number": number, "status": "pending_approval"})
}

// ListWithdrawalRequests handles GET /portal/v1/withdrawal-requests
func (h *PortalHandler) ListWithdrawalRequests(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	ids := h.repo.ScopeIDs(c.Request.Context(), mid)

	items, total, err := h.repo.ListWithdrawalRequests(c.Request.Context(), ids)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.PortalWithdrawalRequestListResponse{Items: items, Total: total})
}

// RejectWithdrawalRequest handles POST /portal/v1/withdrawal-requests/:id/reject
// Creates storno movements (compensating entries) to restore the merchant balance,
// then updates the request status to "rejected". Rejected is a terminal state.
//
// Uses RunInTransaction + SELECT FOR UPDATE to prevent:
//   - Double-storno race condition (CWE-362): concurrent rejects inflating balance
//   - Non-atomic state: storno written but status not updated on partial failure
func (h *PortalHandler) RejectWithdrawalRequest(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	requestID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid request ID"))
		c.Abort()
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation("invalid request body"))
		c.Abort()
		return
	}

	ctx := c.Request.Context()
	ids := h.repo.ScopeIDs(ctx, mid)

	// All steps in a single transaction:
	// SELECT FOR UPDATE → status check → storno INSERT → UPDATE status
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err).WithDetail("missing", "tx_manager"))
		c.Abort()
		return
	}

	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		// 1. SELECT FOR UPDATE — locks the row, concurrent reject waits.
		docID, postedVersion, status, err := h.repo.GetWithdrawalRequestForStorno(txCtx, ids, requestID)
		if err != nil {
			return apperror.NewNotFound("withdrawal request", requestID)
		}

		// 2. Validate status — only pending_approval can be rejected.
		if status != string(withdrawal_request.StatusPendingApproval) {
			return apperror.NewValidation("only pending requests can be rejected")
		}

		// 3. Create storno movements (compensating entries).
		// StornoMovements reads original expense movements and inserts mirrored receipt movements.
		// The DB trigger automatically adjusts the balance on INSERT.
		stornoVersion := postedVersion + 1
		if err := h.merchantBalanceSvc.StornoMovements(txCtx, docID, stornoVersion); err != nil {
			return fmt.Errorf("storno movements: %w", err)
		}

		// 4. Update document status to rejected.
		return h.repo.RejectWithdrawalRequest(txCtx, requestID, req.Reason)
	})
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "rejected"})
}
