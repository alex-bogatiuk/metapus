// Package goods_receipt provides the GoodsReceipt document repository.
package goods_receipt

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines operations for goods receipt documents.
type Repository interface {
	// CRUD operations
	Create(ctx context.Context, doc *GoodsReceipt) error
	GetByID(ctx context.Context, docID id.ID) (*GoodsReceipt, error)
	GetByNumber(ctx context.Context, number string) (*GoodsReceipt, error)
	Update(ctx context.Context, doc *GoodsReceipt) error
	Delete(ctx context.Context, docID id.ID) error
	
	// Line operations
	GetLines(ctx context.Context, docID id.ID) ([]GoodsReceiptLine, error)
	SaveLines(ctx context.Context, docID id.ID, lines []GoodsReceiptLine) error
	
	// List operations
	List(ctx context.Context, filter ListFilter) (domain.ListResult[*GoodsReceipt], error)
	
	// Locking
	GetForUpdate(ctx context.Context, docID id.ID) (*GoodsReceipt, error)
}

// ListFilter for filtering goods receipts.
type ListFilter struct {
	domain.ListFilter
	
	// Document-specific filters
	SupplierID  *id.ID
	WarehouseID *id.ID
	Posted      *bool
	DateFrom    *time.Time
	DateTo      *time.Time
}
