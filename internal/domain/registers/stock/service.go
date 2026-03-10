// Package stock provides the stock accumulation register service.
package stock

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/pkg/logger"
)

// Service provides business operations for the stock register.
// In Database-per-Tenant architecture, transactions are managed by the caller (posting engine).
type Service struct {
	repo Repository
}

// NewService creates a new stock register service.
func NewService(repo Repository) *Service {
	return &Service{
		repo: repo,
	}
}

// RecordMovements records stock movements from a document posting.
// This is called during document posting within a transaction.
func (s *Service) RecordMovements(ctx context.Context, movements []entity.StockMovement) error {
	if len(movements) == 0 {
		return nil
	}

	// Validate movements
	for i, m := range movements {
		if !m.Quantity.IsPositive() {
			return apperror.NewValidation(fmt.Sprintf("movement %d: quantity must be positive", i))
		}
		if id.IsNil(m.RecorderID) {
			return apperror.NewValidation(fmt.Sprintf("movement %d: recorder_id is required", i))
		}
	}

	// Create movements (triggers will update balances)
	if err := s.repo.CreateMovements(ctx, movements); err != nil {
		return fmt.Errorf("create movements: %w", err)
	}

	logger.Info(ctx, "recorded stock movements",
		"count", len(movements),
		"recorder_id", movements[0].RecorderID,
	)

	return nil
}

// ReverseMovements removes movements for a document (used during unposting).
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.DeleteMovementsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("delete movements: %w", err)
	}

	logger.Info(ctx, "reversed stock movements",
		"recorder_id", recorderID,
		"before_version", beforeVersion,
	)

	return nil
}

// CheckAndReserveStock validates stock availability with pessimistic locking.
// Should be called within a transaction before creating expense movements.
// Uses a single batch query (GetBalancesForUpdate) instead of N individual queries.
func (s *Service) CheckAndReserveStock(ctx context.Context, items []StockReservation) error {
	if len(items) == 0 {
		return nil
	}

	// Build BalanceKey slice for batch lookup.
	keys := make([]BalanceKey, len(items))
	for i, item := range items {
		keys[i] = BalanceKey{
			WarehouseID: item.WarehouseID,
			ProductID:   item.ProductID,
		}
	}

	// Single DB roundtrip: lock all rows in deterministic order.
	balances, err := s.repo.GetBalancesForUpdate(ctx, keys)
	if err != nil {
		return fmt.Errorf("get balances for update: %w", err)
	}

	// Build a lookup map: (warehouseID, productID) → balance.
	type dimKey struct {
		w, p id.ID
	}
	balanceMap := make(map[dimKey]types.Quantity, len(balances))
	for _, b := range balances {
		balanceMap[dimKey{b.WarehouseID, b.ProductID}] = b.Quantity
	}

	// Validate each reservation.
	for _, item := range items {
		available := balanceMap[dimKey{item.WarehouseID, item.ProductID}]
		if available < item.RequiredQty {
			return apperror.NewInsufficientStock(
				item.ProductID.String(),
				item.RequiredQty.Float64(),
				available.Float64(),
			)
		}
	}

	return nil
}

// StockReservation represents a stock check request.
type StockReservation struct {
	WarehouseID id.ID
	ProductID   id.ID
	RequiredQty types.Quantity
}

// GetProductAvailability returns available quantity across warehouses.
func (s *Service) GetProductAvailability(ctx context.Context, productID id.ID) (types.Quantity, error) {
	balances, err := s.repo.GetBalancesByProduct(ctx, productID)
	if err != nil {
		return 0, fmt.Errorf("get balances: %w", err)
	}

	var total types.Quantity
	for _, b := range balances {
		total += b.Quantity
	}

	return total, nil
}

// GetWarehouseStock returns all products with stock in a warehouse.
func (s *Service) GetWarehouseStock(ctx context.Context, warehouseID id.ID) ([]entity.StockBalance, error) {
	return s.repo.GetBalancesByWarehouse(ctx, warehouseID, BalanceFilter{
		ExcludeZero: true,
	})
}

// GetStockReport generates a turnover report for the period.
func (s *Service) GetStockReport(ctx context.Context, filter TurnoverFilter) (Turnover, error) {
	return s.repo.GetTurnover(ctx, filter)
}
