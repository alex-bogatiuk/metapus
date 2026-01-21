package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/stock"
	"metapus/internal/infrastructure/http/v1/dto"
)

// StockHandler handles HTTP requests for Stock register.
type StockHandler struct {
	*BaseHandler
	service *stock.Service
	repo    stock.Repository
}

// NewStockHandler creates a new stock register handler.
func NewStockHandler(base *BaseHandler, service *stock.Service, repo stock.Repository) *StockHandler {
	return &StockHandler{
		BaseHandler: base,
		service:     service,
		repo:        repo,
	}
}

// GetBalances handles GET /registers/stock/balances
func (h *StockHandler) GetBalances(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse optional warehouse filter
	var warehouseID *id.ID
	if whStr := c.Query("warehouseId"); whStr != "" {
		parsed, err := id.Parse(whStr)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid warehouseId format"))
			return
		}
		warehouseID = &parsed
	}

	// Parse optional product filter
	var productID *id.ID
	if pStr := c.Query("productId"); pStr != "" {
		parsed, err := id.Parse(pStr)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid productId format"))
			return
		}
		productID = &parsed
	}

	var balances []dto.StockBalanceResponse

	if warehouseID != nil {
		filter := stock.BalanceFilter{
			ExcludeZero: c.Query("excludeZero") != "false",
		}
		if productID != nil {
			filter.ProductIDs = []id.ID{*productID}
		}

		entityBalances, err := h.repo.GetBalancesByWarehouse(ctx, *warehouseID, filter)
		if err != nil {
			h.Error(c, err)
			return
		}

		balances = make([]dto.StockBalanceResponse, len(entityBalances))
		for i, b := range entityBalances {
			balances[i] = dto.FromStockBalance(b)
		}
	} else if productID != nil {
		entityBalances, err := h.repo.GetBalancesByProduct(ctx, *productID)
		if err != nil {
			h.Error(c, err)
			return
		}

		balances = make([]dto.StockBalanceResponse, len(entityBalances))
		for i, b := range entityBalances {
			balances[i] = dto.FromStockBalance(b)
		}
	} else {
		h.Error(c, apperror.NewValidation("warehouseId or productId is required"))
		return
	}

	c.JSON(http.StatusOK, dto.StockBalanceListResponse{Items: balances})
}

// GetMovements handles GET /registers/stock/movements
func (h *StockHandler) GetMovements(c *gin.Context) {
	ctx := c.Request.Context()

	// Product is required for movement history
	productIDStr := c.Query("productId")
	if productIDStr == "" {
		h.Error(c, apperror.NewValidation("productId is required"))
		return
	}

	productID, err := id.Parse(productIDStr)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid productId format"))
		return
	}

	filter := stock.MovementFilter{
		Limit:  h.ParseIntQuery(c, "limit", 100),
		Offset: h.ParseIntQuery(c, "offset", 0),
	}

	// Parse optional warehouse filter
	if whStr := c.Query("warehouseId"); whStr != "" {
		parsed, err := id.Parse(whStr)
		if err == nil {
			filter.WarehouseID = &parsed
		}
	}

	// Parse optional date range
	if fromStr := c.Query("fromDate"); fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			filter.FromDate = &parsed
		}
	}

	if toStr := c.Query("toDate"); toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			filter.ToDate = &parsed
		}
	}

	movements, err := h.repo.GetMovementHistory(ctx, productID, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := make([]dto.StockMovementResponse, len(movements))
	for i, m := range movements {
		response[i] = dto.FromStockMovement(m)
	}

	c.JSON(http.StatusOK, dto.StockMovementListResponse{
		Items:      response,
		TotalCount: len(response),
	})
}

// GetTurnovers handles GET /registers/stock/turnovers
func (h *StockHandler) GetTurnovers(c *gin.Context) {
	ctx := c.Request.Context()

	// Date range is required
	fromStr := c.Query("fromDate")
	toStr := c.Query("toDate")

	if fromStr == "" || toStr == "" {
		h.Error(c, apperror.NewValidation("fromDate and toDate are required"))
		return
	}

	fromDate, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid fromDate format, expected RFC3339"))
		return
	}

	toDate, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid toDate format, expected RFC3339"))
		return
	}

	filter := stock.TurnoverFilter{
		FromDate: fromDate,
		ToDate:   toDate,
	}

	// Parse optional warehouse filter
	if whStr := c.Query("warehouseId"); whStr != "" {
		parsed, err := id.Parse(whStr)
		if err == nil {
			filter.WarehouseID = &parsed
		}
	}

	// Parse optional product filter
	if pStr := c.Query("productId"); pStr != "" {
		parsed, err := id.Parse(pStr)
		if err == nil {
			filter.ProductID = &parsed
		}
	}

	turnover, err := h.service.GetStockReport(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromStockTurnover(turnover))
}

// GetProductAvailability handles GET /registers/stock/availability/:productId
func (h *StockHandler) GetProductAvailability(c *gin.Context) {
	ctx := c.Request.Context()

	productID, err := id.Parse(c.Param("productId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid productId format"))
		return
	}

	quantity, err := h.service.GetProductAvailability(ctx, productID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"productId": productID.String(),
		"quantity":  quantity,
	})
}

// RegisterRoutes registers stock register routes.
func (h *StockHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/balances", h.GetBalances)
	rg.GET("/movements", h.GetMovements)
	rg.GET("/turnovers", h.GetTurnovers)
	rg.GET("/availability/:productId", h.GetProductAvailability)
}
