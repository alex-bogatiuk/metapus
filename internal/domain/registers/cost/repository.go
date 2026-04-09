// Package cost provides the cost accumulation register.
package cost

import (
	"context"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// Repository defines operations for the cost register.
type Repository interface {
	// Movement operations

	// CreateMovements batch inserts movements (used during posting)
	CreateMovements(ctx context.Context, movements []entity.CostMovement) error

	// DeleteMovementsByRecorder removes all movements for a document version
	DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error

	// GetMovementsByRecorder retrieves all movements for a document
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CostMovement, error)

	// Balance operations

	// GetBalance returns current balance for warehouse+product+currency
	GetBalance(ctx context.Context, warehouseID, productID, currencyID id.ID) (entity.CostBalance, error)

	// GetBalancesByWarehouse returns all non-zero balances for a warehouse
	GetBalancesByWarehouse(ctx context.Context, warehouseID id.ID) ([]entity.CostBalance, error)

	// GetBalancesByProduct returns balances across all warehouses for a product
	GetBalancesByProduct(ctx context.Context, productID id.ID) ([]entity.CostBalance, error)
}
