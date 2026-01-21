// Package stock provides the stock accumulation register.
package stock

import (
	"context"
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// Repository defines operations for the stock register.
type Repository interface {
	// Movement operations
	
	// CreateMovements batch inserts movements (used during posting)
	CreateMovements(ctx context.Context, movements []entity.StockMovement) error
	
	// DeleteMovementsByRecorder removes all movements for a document version
	// Used during unposting or re-posting
	DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	
	// GetMovementsByRecorder retrieves all movements for a document
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.StockMovement, error)
	
	// Balance operations
	
	// GetBalance returns current balance for warehouse+product
	GetBalance(ctx context.Context, warehouseID, productID id.ID) (entity.StockBalance, error)
	
	// GetBalanceForUpdate returns balance with row lock for stock control
	GetBalanceForUpdate(ctx context.Context, warehouseID, productID id.ID) (entity.StockBalance, error)
	
	// GetBalancesByWarehouse returns all non-zero balances for a warehouse
	GetBalancesByWarehouse(ctx context.Context, warehouseID id.ID, filter BalanceFilter) ([]entity.StockBalance, error)
	
	// GetBalancesByProduct returns balances across all warehouses for a product
	GetBalancesByProduct(ctx context.Context, productID id.ID) ([]entity.StockBalance, error)
	
	// GetBalancesAtDate calculates balances as of a specific date (for reports)
	GetBalancesAtDate(ctx context.Context, warehouseID, productID id.ID, date time.Time) (float64, error)
	
	// Reporting
	
	// GetMovementHistory returns movement history for a product
	GetMovementHistory(ctx context.Context, productID id.ID, filter MovementFilter) ([]entity.StockMovement, error)
	
	// GetTurnover calculates receipt and expense totals for period
	GetTurnover(ctx context.Context, filter TurnoverFilter) (Turnover, error)
	
	// Maintenance
	
	// RecalculateBalances rebuilds balance table from movements
	RecalculateBalances(ctx context.Context, warehouseID, productID *id.ID) error
}

// BalanceFilter for filtering balance queries.
type BalanceFilter struct {
	ProductIDs    []id.ID
	ExcludeZero   bool
	MinQuantity   *float64
	MaxQuantity   *float64
}

func (f BalanceFilter) minQuantityScaled() *types.Quantity {
	if f.MinQuantity == nil {
		return nil
	}
	q := types.NewQuantityFromFloat64(*f.MinQuantity)
	return &q
}

func (f BalanceFilter) maxQuantityScaled() *types.Quantity {
	if f.MaxQuantity == nil {
		return nil
	}
	q := types.NewQuantityFromFloat64(*f.MaxQuantity)
	return &q
}

// MovementFilter for filtering movement history.
type MovementFilter struct {
	WarehouseID *id.ID
	RecordType  *entity.RecordType
	FromDate    *time.Time
	ToDate      *time.Time
	Limit       int
	Offset      int
}

// TurnoverFilter for turnover reports.
type TurnoverFilter struct {
	WarehouseID *id.ID
	ProductID   *id.ID
	FromDate    time.Time
	ToDate      time.Time
}

// Turnover represents receipt/expense totals.
type Turnover struct {
	WarehouseID    id.ID   `json:"warehouseId,omitempty"`
	ProductID      id.ID   `json:"productId,omitempty"`
	OpeningBalance float64 `json:"openingBalance"`
	Receipt        float64 `json:"receipt"`
	Expense        float64 `json:"expense"`
	ClosingBalance float64 `json:"closingBalance"`
}
