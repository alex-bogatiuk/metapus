package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/domain"
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
//	[{"sourceEntityName":"GoodsReceipt","sourceEntityType":"document",
//	  "sourceField":"counterpartyId","sourceId":"uuid1",
//	  "presentation":"Поступление товаров ПТ-00042 от 15.03.2026"}]
func (h *RefFinderHandler) FindReferences(c *gin.Context) {
	var req domain.FindReferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation("invalid request: " + err.Error()))
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

	c.JSON(http.StatusOK, gin.H{"items": results, "total": len(results)})
}
