package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
)

// PortalHandler handles portal dashboard endpoints.
// Thin adapter: parse params → call repo → return DTO.
type PortalHandler struct {
	repo *portal_repo.DashboardRepo
}

// NewPortalHandler creates a new portal handler.
func NewPortalHandler(repo *portal_repo.DashboardRepo) *PortalHandler {
	return &PortalHandler{repo: repo}
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
