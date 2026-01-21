package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/inventory"
	"metapus/internal/infrastructure/http/v1/dto"
)

// InventoryHandler handles HTTP requests for Inventory documents.
type InventoryHandler struct {
	*BaseHandler
	service *inventory.Service
}

// NewInventoryHandler creates a new inventory handler.
func NewInventoryHandler(base *BaseHandler, service *inventory.Service) *InventoryHandler {
	return &InventoryHandler{
		BaseHandler: base,
		service:     service,
	}
}

func (h *InventoryHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	filter := inventory.ListFilter{
		ListFilter: domain.DefaultListFilter(),
	}
	filter.Search = c.Query("search")
	filter.Limit = h.ParseIntQuery(c, "limit", 50)
	filter.Offset = h.ParseIntQuery(c, "offset", 0)
	filter.OrderBy = c.DefaultQuery("orderBy", "date DESC")
	filter.IncludeDeleted = c.Query("includeDeleted") == "true"

	if warehouseID := c.Query("warehouseId"); warehouseID != "" {
		parsed, err := id.Parse(warehouseID)
		if err == nil {
			filter.WarehouseID = &parsed
		}
	}

	if status := c.Query("status"); status != "" {
		s := inventory.InventoryStatus(status)
		filter.Status = &s
	}

	if posted := c.Query("posted"); posted != "" {
		val := posted == "true"
		filter.Posted = &val
	}

	if dateFrom := c.Query("dateFrom"); dateFrom != "" {
		if parsed, err := time.Parse(time.RFC3339, dateFrom); err == nil {
			filter.DateFrom = &parsed
		}
	}

	if dateTo := c.Query("dateTo"); dateTo != "" {
		if parsed, err := time.Parse(time.RFC3339, dateTo); err == nil {
			filter.DateTo = &parsed
		}
	}

	result, err := h.service.List(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.respondList(c, result)
}

func (h *InventoryHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromInventory(doc))
}

func (h *InventoryHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.CreateInventoryRequest
	if !h.BindJSON(c, &req) {
		return
	}

	doc := req.ToEntity()

	if userID := h.GetUserID(c); userID != "" {
		doc.CreatedBy = userID
		doc.UpdatedBy = userID
	}

	if err := h.service.Create(ctx, doc); err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromInventory(doc)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

func (h *InventoryHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.UpdateInventoryRequest
	if !h.BindJSON(c, &req) {
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	req.ApplyTo(doc)

	if userID := h.GetUserID(c); userID != "" {
		doc.UpdatedBy = userID
	}

	if err := h.service.Update(ctx, doc); err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromInventory(doc)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

func (h *InventoryHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Delete(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// PrepareSheet prepares the inventory sheet with current stock balances.
func (h *InventoryHandler) PrepareSheet(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	doc, err := h.service.PrepareSheet(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromInventory(doc))
}

// Start starts the inventory counting process.
func (h *InventoryHandler) Start(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Start(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromInventory(doc))
}

// RecordCount records the actual quantity for a line.
func (h *InventoryHandler) RecordCount(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.RecordCountRequest
	if !h.BindJSON(c, &req) {
		return
	}

	countedBy := h.GetUserID(c)
	if err := h.service.RecordCount(ctx, docID, req.LineNo, req.ActualQuantity, countedBy); err != nil {
		h.Error(c, err)
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromInventory(doc))
}

// Complete completes the inventory.
func (h *InventoryHandler) Complete(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Complete(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromInventory(doc))
}

// Cancel cancels the inventory.
func (h *InventoryHandler) Cancel(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Cancel(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromInventory(doc))
}

// GetComparison returns the comparison of book vs actual quantities.
func (h *InventoryHandler) GetComparison(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	comparison, err := h.service.GetComparison(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromComparison(comparison))
}

// Post posts the inventory document.
func (h *InventoryHandler) Post(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Post(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromInventory(doc)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Unpost unposts the inventory document.
func (h *InventoryHandler) Unpost(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Unpost(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromInventory(doc)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

func (h *InventoryHandler) respondList(c *gin.Context, result domain.ListResult[*inventory.Inventory]) {
	items := make([]*dto.InventoryResponse, len(result.Items))
	for i, doc := range result.Items {
		items[i] = dto.FromInventory(doc)
	}

	c.JSON(http.StatusOK, dto.InventoryListResponse{
		Items:      items,
		TotalCount: int(result.TotalCount),
		Limit:      result.Limit,
		Offset:     result.Offset,
	})
}
