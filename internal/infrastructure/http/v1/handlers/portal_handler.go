package handlers

import (
	"math/big"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
)

// PortalHandler handles portal dashboard endpoints.
// Thin adapter: parse params → call repo/service → return DTO.
type PortalHandler struct {
	repo               *portal_repo.DashboardRepo
	apiKeyRepo         merchant.APIKeyRepository
	balanceCalc        *crypto.BalanceCalculator // nil-safe: returns empty balance if not configured
	rateSourceResolver crypto.RateSourceResolver // resolves source code → UUID
}

// NewPortalHandler creates a new portal handler.
func NewPortalHandler(
	repo *portal_repo.DashboardRepo,
	apiKeyRepo merchant.APIKeyRepository,
	balanceCalc *crypto.BalanceCalculator,
	rateSourceResolver crypto.RateSourceResolver,
) *PortalHandler {
	return &PortalHandler{
		repo:               repo,
		apiKeyRepo:         apiKeyRepo,
		balanceCalc:        balanceCalc,
		rateSourceResolver: rateSourceResolver,
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

	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	items, total, err := h.repo.ListInvoices(c.Request.Context(), mid, status, limit, offset)
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

