package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/http/v1/dto"
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

// ResolveRefs resolves a batch of typed references into presentations.
// POST /api/v1/resolve-refs
//
// Request body:
//
//	{
//	  "refs": [
//	    {"refType": "CryptoInvoice", "refId": "uuid1"},
//	    {"refType": "Counterparty", "refId": "uuid2"}
//	  ]
//	}
//
// Response:
//
//	[
//	  {"refType": "CryptoInvoice", "refId": "uuid1", "presentation": "Крипто-инвойс CI-00042 от 15.03.2026", "entityType": "document"},
//	  {"refType": "Counterparty", "refId": "uuid2", "presentation": "ООО Рога и Копыта (К-0001)", "entityType": "catalog"}
//	]
func (h *RefResolverHandler) ResolveRefs(c *gin.Context) {
	var req dto.ResolveRefsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
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
