package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// GoodsReceiptHandler handles HTTP requests for GoodsReceipt documents.
// Overrides all response methods to resolve reference IDs → display names.
type GoodsReceiptHandler struct {
	*BaseDocumentHandler[*goods_receipt.GoodsReceipt, dto.CreateGoodsReceiptRequest, dto.UpdateGoodsReceiptRequest]
	service domain.DocumentService[*goods_receipt.GoodsReceipt]
}

// NewGoodsReceiptHandler creates a new goods receipt handler.
// Accepts domain.DocumentService interface — can be a concrete service or a decorated wrapper.
func NewGoodsReceiptHandler(base *BaseHandler, service domain.DocumentService[*goods_receipt.GoodsReceipt]) *GoodsReceiptHandler {
	cfg := BaseDocumentHandlerConfig[*goods_receipt.GoodsReceipt, dto.CreateGoodsReceiptRequest, dto.UpdateGoodsReceiptRequest]{
		Service:    service,
		EntityName: "goods_receipt",
		MapCreateDTO: func(req dto.CreateGoodsReceiptRequest) *goods_receipt.GoodsReceipt {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateGoodsReceiptRequest, existing *goods_receipt.GoodsReceipt) *goods_receipt.GoodsReceipt {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *goods_receipt.GoodsReceipt) any {
			return dto.FromGoodsReceipt(entity, nil)
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

// resolveDocRefs batch-resolves all reference IDs for a list of documents.
// Returns ResolvedRefs + ResolvedCurrencyRefs (with decimalPlaces, symbol).
func (h *GoodsReceiptHandler) resolveDocRefs(ctx context.Context, docs ...*goods_receipt.GoodsReceipt) (postgres.ResolvedRefs, postgres.ResolvedCurrencyRefs, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		dto.CollectGoodsReceiptRefs(resolver, doc)
	}

	pool := tenant.MustGetPool(ctx)
	refs, err := resolver.Resolve(ctx, pool)
	if err != nil {
		return nil, nil, err
	}
	currencyRefs, err := resolver.ResolveCurrencies(ctx, pool)
	if err != nil {
		return nil, nil, err
	}
	return refs, currencyRefs, nil
}

// Get handles GET /document/goods-receipt/:id — with resolved references.
func (h *GoodsReceiptHandler) Get(c *gin.Context) {
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

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	refs, currencyRefs, err := h.resolveDocRefs(ctx, doc)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromGoodsReceipt(doc, refs, currencyRefs))
}

// List handles GET /document/goods-receipt — with resolved references.
func (h *GoodsReceiptHandler) List(c *gin.Context) {
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
	refs, currencyRefs, err := h.resolveDocRefs(ctx, result.Items...)
	if err != nil {
		h.Error(c, err)
		return
	}

	// FLS: mask restricted fields before DTO mapping
	policy := security.GetFieldPolicy(ctx, h.entityName, "read")
	masker := security.NewFieldMasker()

	items := make([]any, len(result.Items))
	for i, item := range result.Items {
		if policy != nil {
			masker.MaskForRead(item, policy)
		}
		items[i] = dto.FromGoodsReceipt(item, refs, currencyRefs)
	}

	c.JSON(http.StatusOK, dto.CursorListResponse{
		Items:       items,
		NextCursor:  result.NextCursor,
		PrevCursor:  result.PrevCursor,
		HasMore:     result.HasMore,
		HasPrev:     result.HasPrev,
		TargetIndex: result.TargetIndex,
		TotalCount:  result.TotalCount,
	})
}

// Create handles POST /document/goods-receipt — with resolved references.
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

	refs, currencyRefs, _ := h.resolveDocRefs(ctx, doc)
	response := dto.FromGoodsReceipt(doc, refs, currencyRefs)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// Update handles PUT /document/goods-receipt/:id — with resolved references.
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

	refs, currencyRefs, _ := h.resolveDocRefs(ctx, doc)
	response := dto.FromGoodsReceipt(doc, refs, currencyRefs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Post handles POST /document/goods-receipt/:id/post — with resolved references.
func (h *GoodsReceiptHandler) Post(c *gin.Context) {
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

	refs, currencyRefs, _ := h.resolveDocRefs(ctx, doc)
	response := dto.FromGoodsReceipt(doc, refs, currencyRefs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Unpost handles POST /document/goods-receipt/:id/unpost — with resolved references.
func (h *GoodsReceiptHandler) Unpost(c *gin.Context) {
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

	refs, currencyRefs, _ := h.resolveDocRefs(ctx, doc)
	response := dto.FromGoodsReceipt(doc, refs, currencyRefs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// SetDeletionMark handles POST /document/goods-receipt/:id/deletion-mark — with resolved references.
func (h *GoodsReceiptHandler) SetDeletionMark(c *gin.Context) {
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

	refs, currencyRefs, _ := h.resolveDocRefs(ctx, doc)
	response := dto.FromGoodsReceipt(doc, refs, currencyRefs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// UpdateAndRepost handles PUT /document/goods-receipt/:id/repost — atomic update + re-post.
// Accepts the same body as Update. The document is updated and re-posted in a single transaction.
func (h *GoodsReceiptHandler) UpdateAndRepost(c *gin.Context) {
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

	if err := h.service.UpdateAndRepost(ctx, doc); err != nil {
		h.Error(c, err)
		return
	}

	refs, currencyRefs, _ := h.resolveDocRefs(ctx, doc)
	response := dto.FromGoodsReceipt(doc, refs, currencyRefs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Copy handles POST /document/goods-receipt/:id/copy — with resolved references.
func (h *GoodsReceiptHandler) Copy(c *gin.Context) {
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

	copy := goods_receipt.NewGoodsReceipt(source.OrganizationID, source.SupplierID, source.WarehouseID)
	copy.Date = time.Now()
	copy.ContractID = source.ContractID
	copy.SupplierDocNumber = source.SupplierDocNumber
	copy.IncomingNumber = source.IncomingNumber
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

	refs, currencyRefs, _ := h.resolveDocRefs(ctx, copy)
	response := dto.FromGoodsReceipt(copy, refs, currencyRefs)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// RegisterRoutes registers goods receipt routes.
// All methods are overridden to include reference resolution.
func (h *GoodsReceiptHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("", h.List)
	rg.POST("", h.Create)
	rg.GET("/:id", h.Get)
	rg.PUT("/:id", h.Update)
	rg.DELETE("/:id", h.BaseDocumentHandler.Delete)
	rg.POST("/:id/post", h.Post)
	rg.POST("/:id/unpost", h.Unpost)
	rg.POST("/:id/deletion-mark", h.SetDeletionMark)
	rg.POST("/:id/copy", h.Copy)
}
