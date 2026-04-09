package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/reports"
	"metapus/internal/infrastructure/http/v1/dto"
)

// ReportsHandler handles HTTP requests for reports.
type ReportsHandler struct {
	*BaseHandler
	service *reports.Service
}

// NewReportsHandler creates a new reports handler.
func NewReportsHandler(base *BaseHandler, service *reports.Service) *ReportsHandler {
	return &ReportsHandler{
		BaseHandler: base,
		service:     service,
	}
}

// GetStockBalance handles GET /reports/stock-balance
func (h *ReportsHandler) GetStockBalance(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.StockBalanceReportRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		h.Error(c, apperror.NewValidation(err.Error()))
		return
	}

	filter := reports.StockBalanceReportFilter{
		AsOfDate:    req.AsOfDate,
		ExcludeZero: req.ExcludeZero == nil || *req.ExcludeZero,
		Limit:       req.Limit,
		Offset:      req.Offset,
	}

	// Parse warehouse IDs
	for _, whStr := range req.WarehouseIDs {
		if whID, err := id.Parse(whStr); err == nil {
			filter.WarehouseIDs = append(filter.WarehouseIDs, whID)
		}
	}

	// Parse product IDs
	for _, pStr := range req.ProductIDs {
		if pID, err := id.Parse(pStr); err == nil {
			filter.ProductIDs = append(filter.ProductIDs, pID)
		}
	}

	report, err := h.service.GetStockBalance(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromStockBalanceReport(report))
}

// GetStockTurnover handles GET /reports/stock-turnover
func (h *ReportsHandler) GetStockTurnover(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.StockTurnoverReportRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		h.Error(c, apperror.NewValidation(err.Error()))
		return
	}

	fromDate, err := time.Parse(time.RFC3339, req.FromDate)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid fromDate format, expected RFC3339"))
		return
	}

	toDate, err := time.Parse(time.RFC3339, req.ToDate)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid toDate format, expected RFC3339"))
		return
	}

	filter := reports.StockTurnoverReportFilter{
		FromDate:    fromDate,
		ToDate:      toDate,
		IncludeZero: req.IncludeZero,
		Limit:       req.Limit,
		Offset:      req.Offset,
	}

	// Parse warehouse IDs
	for _, whStr := range req.WarehouseIDs {
		if whID, err := id.Parse(whStr); err == nil {
			filter.WarehouseIDs = append(filter.WarehouseIDs, whID)
		}
	}

	// Parse product IDs
	for _, pStr := range req.ProductIDs {
		if pID, err := id.Parse(pStr); err == nil {
			filter.ProductIDs = append(filter.ProductIDs, pID)
		}
	}

	report, err := h.service.GetStockTurnover(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromStockTurnoverReport(report))
}

// GetDocumentJournal handles GET /reports/document-journal
func (h *ReportsHandler) GetDocumentJournal(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.DocumentJournalRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		h.Error(c, apperror.NewValidation(err.Error()))
		return
	}

	filter := reports.DocumentJournalFilter{
		DocumentTypes:  req.DocumentTypes,
		Posted:         req.Posted,
		NumberContains: req.NumberContains,
		SortBy:         req.SortBy,
		SortOrder:      req.SortOrder,
		Limit:          req.Limit,
		Offset:         req.Offset,
	}

	// Parse dates
	if req.FromDate != nil {
		if t, err := time.Parse(time.RFC3339, *req.FromDate); err == nil {
			filter.FromDate = &t
		}
	}
	if req.ToDate != nil {
		if t, err := time.Parse(time.RFC3339, *req.ToDate); err == nil {
			filter.ToDate = &t
		}
	}

	// Parse warehouse IDs
	for _, whStr := range req.WarehouseIDs {
		if whID, err := id.Parse(whStr); err == nil {
			filter.WarehouseIDs = append(filter.WarehouseIDs, whID)
		}
	}

	// Parse supplier IDs
	for _, sStr := range req.SupplierIDs {
		if sID, err := id.Parse(sStr); err == nil {
			filter.SupplierIDs = append(filter.SupplierIDs, sID)
		}
	}

	journal, err := h.service.GetDocumentJournal(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromDocumentJournal(journal))
}

// RegisterRoutes registers report routes.
func (h *ReportsHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/stock-balance", h.GetStockBalance)
	rg.GET("/stock-turnover", h.GetStockTurnover)
	rg.GET("/document-journal", h.GetDocumentJournal)
}
