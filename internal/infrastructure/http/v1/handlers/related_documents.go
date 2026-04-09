package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// RelatedDocumentsHandler handles GET /:id/related-documents requests.
// Mounted on each document type's route group automatically.
type RelatedDocumentsHandler struct {
	finder     domain.RelatedDocFinder
	entityName string
}

// NewRelatedDocumentsHandler creates a new handler.
func NewRelatedDocumentsHandler(finder domain.RelatedDocFinder, entityName string) *RelatedDocumentsHandler {
	return &RelatedDocumentsHandler{
		finder:     finder,
		entityName: entityName,
	}
}

// GetRelatedDocuments handles GET /document/{type}/:id/related-documents.
//
// Response:
//
//	{
//	  "tree": {"id": "uuid", "entityName": "GoodsReceipt", "isCurrent": true, "children": [...]},
//	  "self": {"id": "uuid", "presentation": "ПТ-00015  15.03.2026", ...},
//	  "flatGroups": [...],
//	  "total": 5
//	}
func (h *RelatedDocumentsHandler) GetRelatedDocuments(c *gin.Context) {
	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid id format"))
		c.Abort()
		return
	}

	result, err := h.finder.FindRelatedDocuments(c.Request.Context(), domain.RelatedDocumentsRequest{
		EntityName: h.entityName,
		EntityID:   docID,
	})
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if result == nil {
		result = &domain.RelatedDocumentsResult{}
	}

	c.JSON(http.StatusOK, result)
}
