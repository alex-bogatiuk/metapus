package goods_issue

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines operations for goods issue documents.
type Repository interface {
	Create(ctx context.Context, doc *GoodsIssue) error
	GetByID(ctx context.Context, docID id.ID) (*GoodsIssue, error)
	GetByNumber(ctx context.Context, number string) (*GoodsIssue, error)
	Update(ctx context.Context, doc *GoodsIssue) error
	Delete(ctx context.Context, docID id.ID) error

	GetLines(ctx context.Context, docID id.ID) ([]GoodsIssueLine, error)
	SaveLines(ctx context.Context, docID id.ID, lines []GoodsIssueLine) error

	// List operations — uses universal filter engine via domain.ListFilter.AdvancedFilters
	List(ctx context.Context, filter domain.ListFilter) (domain.ListResult[*GoodsIssue], error)
	GetForUpdate(ctx context.Context, docID id.ID) (*GoodsIssue, error)
}
