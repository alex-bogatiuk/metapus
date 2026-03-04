package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
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
			return dto.FromGoodsIssue(entity, nil)
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

// resolveDocRefs batch-resolves all reference IDs for a list of documents.
// Returns ResolvedRefs that can be passed to dto.FromGoodsIssue.
func (h *GoodsIssueHandler) resolveDocRefs(ctx context.Context, docs ...*goods_issue.GoodsIssue) (postgres.ResolvedRefs, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		dto.CollectGoodsIssueRefs(resolver, doc)
	}

	pool := tenant.MustGetPool(ctx)
	querier := postgres.NewTxManagerFromRawPool(pool).GetQuerier(ctx)

	return resolver.Resolve(ctx, querier)
}

// Get handles GET /document/goods-issue/:id — with resolved references.
func (h *GoodsIssueHandler) Get(c *gin.Context) {
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

	refs, err := h.resolveDocRefs(ctx, doc)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromGoodsIssue(doc, refs))
}

// List handles GET /document/goods-issue — with resolved references.
func (h *GoodsIssueHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	filter, err := h.ParseListFilter(c, "-date")
	if err != nil {
		h.Error(c, err)
		return
	}

	result, err := h.service.List(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Batch-resolve references for all documents in the list
	refs, err := h.resolveDocRefs(ctx, result.Items...)
	if err != nil {
		h.Error(c, err)
		return
	}

	items := make([]any, len(result.Items))
	for i, item := range result.Items {
		items[i] = dto.FromGoodsIssue(item, refs)
	}

	c.JSON(http.StatusOK, dto.ListResponse{
		Items:      items,
		TotalCount: result.TotalCount,
		Limit:      result.Limit,
		Offset:     result.Offset,
	})
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

	refs, err := h.resolveDocRefs(ctx, doc)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsIssue(doc, refs)
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

	refs, err := h.resolveDocRefs(ctx, doc)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsIssue(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Post handles POST /document/goods-issue/:id/post — with resolved references.
func (h *GoodsIssueHandler) Post(c *gin.Context) {
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

	refs, err := h.resolveDocRefs(ctx, doc)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsIssue(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Unpost handles POST /document/goods-issue/:id/unpost — with resolved references.
func (h *GoodsIssueHandler) Unpost(c *gin.Context) {
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

	refs, err := h.resolveDocRefs(ctx, doc)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsIssue(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// SetDeletionMark handles POST /document/goods-issue/:id/deletion-mark — with resolved references.
func (h *GoodsIssueHandler) SetDeletionMark(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.SetDeletionMarkRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := h.service.SetDeletionMark(ctx, docID, req.Marked); err != nil {
		h.Error(c, err)
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	refs, err := h.resolveDocRefs(ctx, doc)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsIssue(doc, refs)
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

	refs, err := h.resolveDocRefs(ctx, copy)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := dto.FromGoodsIssue(copy, refs)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// RegisterRoutes registers goods issue routes.
func (h *GoodsIssueHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.DELETE("/:id", h.BaseDocumentHandler.Delete)

	// Overridden methods to include reference resolution
	rg.GET("/:id", h.Get)
	rg.GET("", h.List)
	rg.POST("", h.Create)
	rg.PUT("/:id", h.Update)
	rg.POST("/:id/post", h.Post)
	rg.POST("/:id/unpost", h.Unpost)
	rg.POST("/:id/deletion-mark", h.SetDeletionMark)

	// Entity-specific methods
	rg.POST("/:id/copy", h.Copy)
}
