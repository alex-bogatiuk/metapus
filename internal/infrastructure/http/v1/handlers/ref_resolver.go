package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/domain"
)

// RefResolverHandler handles batch typed reference resolution.
// POST /api/v1/resolve-refs
type RefResolverHandler struct {
	resolver domain.RefResolver
}

// NewRefResolverHandler creates a new RefResolverHandler.
func NewRefResolverHandler(resolver domain.RefResolver) *RefResolverHandler {
	return &RefResolverHandler{resolver: resolver}
}

// resolveRefsRequest is the request body for batch ref resolution.
type resolveRefsRequest struct {
	Refs []domain.RefResolveRequest `json:"refs" binding:"required,min=1,max=100"`
}

// ResolveRefs resolves a batch of typed references into presentations.
// POST /api/v1/resolve-refs
//
// Request body:
//
//	{
//	  "refs": [
//	    {"refType": "GoodsReceipt", "refId": "uuid1"},
//	    {"refType": "Counterparty", "refId": "uuid2"}
//	  ]
//	}
//
// Response:
//
//	[
//	  {"refType": "GoodsReceipt", "refId": "uuid1", "presentation": "Поступление товаров ПТ-00042 от 15.03.2026", "entityType": "document"},
//	  {"refType": "Counterparty", "refId": "uuid2", "presentation": "ООО Рога и Копыта (К-0001)", "entityType": "catalog"}
//	]
func (h *RefResolverHandler) ResolveRefs(c *gin.Context) {
	var req resolveRefsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation("invalid request: " + err.Error()))
		c.Abort()
		return
	}

	results, err := h.resolver.ResolveRefs(c.Request.Context(), req.Refs)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, results)
}
