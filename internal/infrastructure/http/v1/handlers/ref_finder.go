package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
)

// RefFinderHandler handles "Find References" requests.
// POST /api/v1/system/find-references
type RefFinderHandler struct {
	finder domain.RefFinder
}

// NewRefFinderHandler creates a new RefFinderHandler.
func NewRefFinderHandler(finder domain.RefFinder) *RefFinderHandler {
	return &RefFinderHandler{finder: finder}
}

// FindReferences handles POST /system/find-references.
//
// Request:
//
//	{"entityName": "Counterparty", "entityId": "uuid"}
//
// Response:
//
//	[{"sourceEntityName":"CryptoInvoice","sourceEntityType":"document",
//	  "sourceField":"merchantId","sourceId":"uuid1",
//	  "presentation":"Крипто-инвойс CI-00042 от 15.03.2026"}]
func (h *RefFinderHandler) FindReferences(c *gin.Context) {
	var req domain.FindReferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	results, err := h.finder.FindReferences(c.Request.Context(), req)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if results == nil {
		results = []domain.FoundReference{}
	}

	c.JSON(http.StatusOK, dto.FindReferencesResponse{Items: results, Total: len(results)})
}
