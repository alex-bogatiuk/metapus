package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/infrastructure/http/v1/dto"
)

// GoodsIssueHandler handles HTTP requests for GoodsIssue documents.
// List() is inherited from BaseDocumentHandler (universal filter engine).
type GoodsIssueHandler struct {
	*BaseDocumentHandler[*goods_issue.GoodsIssue, dto.CreateGoodsIssueRequest, dto.UpdateGoodsIssueRequest]
	service *goods_issue.Service
}

// NewGoodsIssueHandler creates a new goods issue handler.
func NewGoodsIssueHandler(base *BaseHandler, service *goods_issue.Service) *GoodsIssueHandler {
	cfg := BaseDocumentHandlerConfig[*goods_issue.GoodsIssue, dto.CreateGoodsIssueRequest, dto.UpdateGoodsIssueRequest]{
		Service:    service,
		EntityName: "goods-issue",
		MapCreateDTO: func(req dto.CreateGoodsIssueRequest) *goods_issue.GoodsIssue {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateGoodsIssueRequest, existing *goods_issue.GoodsIssue) *goods_issue.GoodsIssue {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *goods_issue.GoodsIssue) any {
			return dto.FromGoodsIssue(entity)
		},
		IsPostImmediately: func(req dto.CreateGoodsIssueRequest) bool {
			return req.PostImmediately
		},
	}

	return &GoodsIssueHandler{
		BaseDocumentHandler: NewBaseDocumentHandler(base, cfg),
		service:             service,
	}
}

// Create override to handle UserID injection
func (h *GoodsIssueHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.CreateGoodsIssueRequest
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

	response := dto.FromGoodsIssue(doc)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// Update override to handle UserID injection
func (h *GoodsIssueHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()
	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.UpdateGoodsIssueRequest
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

	response := dto.FromGoodsIssue(doc)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

func (h *GoodsIssueHandler) Copy(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	source, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// No tenantID needed in Database-per-Tenant
	copy := goods_issue.NewGoodsIssue(source.OrganizationID, source.CustomerID, source.WarehouseID)
	copy.Date = time.Now()
	copy.ContractID = source.ContractID
	copy.CustomerOrderNumber = source.CustomerOrderNumber
	copy.CurrencyID = source.CurrencyID
	copy.AmountIncludesVAT = source.AmountIncludesVAT
	copy.Description = source.Description

	for _, line := range source.Lines {
		copy.AddLine(line.ProductID, line.UnitID, line.Coefficient, line.Quantity, line.UnitPrice, line.VATRateID, 0, line.DiscountPercent)
	}
	if err := h.service.Create(ctx, copy); err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsIssue(copy)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// RegisterRoutes registers goods issue routes.
func (h *GoodsIssueHandler) RegisterRoutes(rg *gin.RouterGroup) {
	// Standard methods from Base (includes List)
	rg.GET("/:id", h.BaseDocumentHandler.Get)
	rg.DELETE("/:id", h.BaseDocumentHandler.Delete)
	rg.POST("/:id/post", h.BaseDocumentHandler.Post)
	rg.POST("/:id/unpost", h.BaseDocumentHandler.Unpost)
	rg.POST("/:id/deletion-mark", h.BaseDocumentHandler.SetDeletionMark)

	// List uses base handler's universal filter engine
	rg.GET("", h.BaseDocumentHandler.List)

	// Overridden methods
	rg.POST("", h.Create)
	rg.PUT("/:id", h.Update)

	// Entity-specific methods
	rg.POST("/:id/copy", h.Copy)
}
