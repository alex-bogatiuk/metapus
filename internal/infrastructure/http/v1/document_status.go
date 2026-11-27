package v1

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

// DocumentStatusHistoryHandler serves GET /:id/status-history of a document
// with a status model (entity.StatusModel); nil when it declares none.
type DocumentStatusHistoryHandler interface {
	StatusHistory() gin.HandlerFunc
}

// statusHistoryHandler answers with the status history of a document of
// def. The document is read first through get, so a user sees the history
// of only the documents the record policies let them read, and only when
// the read field policy shows them the status.
func statusHistoryHandler[PT any](def entity.Definition, get func(context.Context, id.ID) (PT, error)) gin.HandlerFunc {
	model := def.Statuses
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		docID, err := id.Parse(c.Param("id"))
		if err != nil {
			_ = c.Error(apperror.NewValidation("invalid id format"))
			c.Abort()
			return
		}
		if _, err := get(ctx, docID); err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		if policy := security.GetFieldPolicy(ctx, def.Key, "read"); policy != nil && !policy.IsFieldAllowed(model.Column) {
			c.JSON(http.StatusOK, dto.DocumentStatusHistoryResponse{Items: []dto.DocumentStatusChange{}})
			return
		}
		history, err := postgres.DocumentStatusHistory(ctx, def.Key, docID)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		var authors []id.ID
		for _, change := range history {
			if change.ChangedBy != nil {
				authors = append(authors, *change.ChangedBy)
			}
		}
		cards, err := postgres.UserCards(ctx, authors)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		items := make([]dto.DocumentStatusChange, len(history))
		for i, change := range history {
			item := dto.DocumentStatusChange{
				Status: change.Status, StatusLabel: model.Label(change.Status),
				PreviousStatus: change.PreviousStatus, ChangedAt: change.ChangedAt,
			}
			if change.PreviousStatus != "" {
				item.PreviousStatusLabel = model.Label(change.PreviousStatus)
			}
			if change.ChangedBy != nil {
				item.ChangedBy = change.ChangedBy.String()
				item.ChangedByName = cards[*change.ChangedBy].DisplayName
			}
			items[i] = item
		}
		c.JSON(http.StatusOK, dto.DocumentStatusHistoryResponse{Items: items})
	}
}
