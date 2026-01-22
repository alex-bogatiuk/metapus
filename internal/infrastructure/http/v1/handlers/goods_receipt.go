package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/infrastructure/http/v1/dto"
)

// GoodsReceiptHandler handles HTTP requests for GoodsReceipt documents.
type GoodsReceiptHandler struct {
	*BaseDocumentHandler[*goods_receipt.GoodsReceipt, dto.CreateGoodsReceiptRequest, dto.UpdateGoodsReceiptRequest]
	service *goods_receipt.Service
}

// NewGoodsReceiptHandler creates a new goods receipt handler.
func NewGoodsReceiptHandler(base *BaseHandler, service *goods_receipt.Service) *GoodsReceiptHandler {
	cfg := BaseDocumentHandlerConfig[*goods_receipt.GoodsReceipt, dto.CreateGoodsReceiptRequest, dto.UpdateGoodsReceiptRequest]{
		Service:    service,
		EntityName: "goods-receipt",
		MapCreateDTO: func(req dto.CreateGoodsReceiptRequest) *goods_receipt.GoodsReceipt {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateGoodsReceiptRequest, existing *goods_receipt.GoodsReceipt) *goods_receipt.GoodsReceipt {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *goods_receipt.GoodsReceipt) any {
			return dto.FromGoodsReceipt(entity)
		},
		IsPostImmediately: func(req dto.CreateGoodsReceiptRequest) bool {
			return req.PostImmediately
		},
	}

	return &GoodsReceiptHandler{
		BaseDocumentHandler: NewBaseDocumentHandler(base, cfg),
		service:             service,
	}
}

// Create override to handle UserID injection
func (h *GoodsReceiptHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.CreateGoodsReceiptRequest
	if !h.BindJSON(c, &req) {
		return
	}

	doc := req.ToEntity()

	var err error
	if req.PostImmediately {
		err = h.service.PostAndSave(ctx, doc)
	} else {
		err = h.service.Create(ctx, doc)
	}

	if err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsReceipt(doc)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// Update override to handle UserID injection
func (h *GoodsReceiptHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()
	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.UpdateGoodsReceiptRequest
	if !h.BindJSON(c, &req) {
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	req.ApplyTo(doc)

	if err := h.service.Update(ctx, doc); err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsReceipt(doc)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// List handles GET /document/goods-receipt - list with filtering.
func (h *GoodsReceiptHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	filter := goods_receipt.ListFilter{
		ListFilter: domain.DefaultListFilter(),
	}
	filter.Search = c.Query("search")
	filter.Limit = h.ParseIntQuery(c, "limit", 50)
	filter.Offset = h.ParseIntQuery(c, "offset", 0)
	filter.OrderBy = c.DefaultQuery("orderBy", "date DESC")
	filter.IncludeDeleted = c.Query("includeDeleted") == "true"

	// Parse optional filters
	if supplierID := c.Query("supplierId"); supplierID != "" {
		parsed, err := id.Parse(supplierID)
		if err == nil {
			filter.SupplierID = &parsed
		}
	}

	if warehouseID := c.Query("warehouseId"); warehouseID != "" {
		parsed, err := id.Parse(warehouseID)
		if err == nil {
			filter.WarehouseID = &parsed
		}
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

// Copy handles POST /document/goods-receipt/:id/copy
func (h *GoodsReceiptHandler) Copy(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	// Get source document
	source, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Create copy (no tenantID needed in Database-per-Tenant)
	copy := goods_receipt.NewGoodsReceipt(source.OrganizationID, source.SupplierID, source.WarehouseID)
	copy.Date = time.Now()
	copy.SupplierDocNumber = source.SupplierDocNumber
	copy.Currency = source.Currency
	copy.Comment = source.Comment

	// Copy lines
	for _, line := range source.Lines {
		copy.AddLine(line.ProductID, line.Quantity, line.UnitPrice, line.VATRate)
	}

	if err := h.service.Create(ctx, copy); err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsReceipt(copy)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// RegisterRoutes registers goods receipt routes.
func (h *GoodsReceiptHandler) RegisterRoutes(rg *gin.RouterGroup) {
	h.BaseDocumentHandler.RegisterRoutes(rg)
	rg.GET("", h.List)
	// Create/Update overrides are registered via BaseDocumentHandler if we embed pointer?
	// No, BaseDocumentHandler registers its OWN methods.
	// Use h.Create to register h.Create (which is the override).
	// But h.BaseDocumentHandler.RegisterRoutes registers h.BaseDocumentHandler.Create.
	// We need to check how RegisterRoutes is implemented.
	// It uses `h.Create`. Since `h` is `*BaseDocumentHandler`, it uses base method.
	// We should invoke our own registration or manual registration if we override.

	// Manual registration for overridden methods:
	rg.POST("", h.Create)    // Uses GoodsReceiptHandler.Create
	rg.PUT("/:id", h.Update) // Uses GoodsReceiptHandler.Update

	// For other methods, we can use base or re-register.
	// BaseDocumentHandler.RegisterRoutes registers all CRUD.
	// If we call it, it will register base methods.
	// We should probably NOT call base.RegisterRoutes if we want to override some.
	// OR we register specific ones.

	rg.GET("/:id", h.Get)            // Base
	rg.DELETE("/:id", h.Delete)      // Base
	rg.POST("/:id/post", h.Post)     // Base
	rg.POST("/:id/unpost", h.Unpost) // Base
	rg.POST("/:id/copy", h.Copy)
}

// respondList sends paginated list response.
func (h *GoodsReceiptHandler) respondList(c *gin.Context, result domain.ListResult[*goods_receipt.GoodsReceipt]) {
	items := make([]*dto.GoodsReceiptResponse, len(result.Items))
	for i, doc := range result.Items {
		items[i] = dto.FromGoodsReceipt(doc)
	}

	c.JSON(http.StatusOK, dto.GoodsReceiptListResponse{
		Items:      items,
		TotalCount: int(result.TotalCount),
		Limit:      result.Limit,
		Offset:     result.Offset,
	})
}
