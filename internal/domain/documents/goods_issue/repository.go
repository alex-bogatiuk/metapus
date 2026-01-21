package goods_issue

import (
	"context"
	"time"

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

	List(ctx context.Context, filter ListFilter) (domain.ListResult[*GoodsIssue], error)
	GetForUpdate(ctx context.Context, docID id.ID) (*GoodsIssue, error)
}

// ListFilter for filtering goods issues.
type ListFilter struct {
	domain.ListFilter

	CustomerID  *id.ID
	WarehouseID *id.ID
	Posted      *bool
	DateFrom    *time.Time
	DateTo      *time.Time
}
