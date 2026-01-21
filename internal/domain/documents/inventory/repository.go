package inventory

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Repository defines operations for inventory documents.
type Repository interface {
	Create(ctx context.Context, doc *Inventory) error
	GetByID(ctx context.Context, docID id.ID) (*Inventory, error)
	Update(ctx context.Context, doc *Inventory) error
	Delete(ctx context.Context, docID id.ID) error

	GetLines(ctx context.Context, docID id.ID) ([]InventoryLine, error)
	SaveLines(ctx context.Context, docID id.ID, lines []InventoryLine) error

	List(ctx context.Context, filter ListFilter) (domain.ListResult[*Inventory], error)
	GetForUpdate(ctx context.Context, docID id.ID) (*Inventory, error)
}

// ListFilter for filtering inventories.
type ListFilter struct {
	domain.ListFilter

	WarehouseID *id.ID
	Status      *InventoryStatus
	Posted      *bool
	DateFrom    *time.Time
	DateTo      *time.Time
}
