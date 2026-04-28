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
			NomenclatureID:   item.NomenclatureID,
		}
	}

	// Single DB roundtrip: lock all rows in deterministic order.
	balances, err := s.repo.GetBalancesForUpdate(ctx, keys)
	if err != nil {
		return fmt.Errorf("get balances for update: %w", err)
	}

	// Build a lookup map: (warehouseID, nomenclatureID) → balance.
	type dimKey struct {
		w, p id.ID
	}
	balanceMap := make(map[dimKey]types.Quantity, len(balances))
	for _, b := range balances {
		balanceMap[dimKey{b.WarehouseID, b.NomenclatureID}] = b.Quantity
	}

	// Validate each reservation.
	for _, item := range items {
		available := balanceMap[dimKey{item.WarehouseID, item.NomenclatureID}]
		if available < item.RequiredQty {
			return apperror.NewInsufficientStock(
				item.NomenclatureID.String(),
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
	NomenclatureID   id.ID
	RequiredQty types.Quantity
}

// GetNomenclatureAvailability returns available quantity across warehouses.
func (s *Service) GetNomenclatureAvailability(ctx context.Context, nomenclatureID id.ID) (types.Quantity, error) {
	balances, err := s.repo.GetBalancesByNomenclature(ctx, nomenclatureID)
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

// ---------------------------------------------------------------------------
// Implementation of entity.MovementProvider
// ---------------------------------------------------------------------------

func (s *Service) RegisterName() string {
	return "Товары на складах"
}

func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return nil, fmt.Errorf("get stock movements: %w", err)
	}

	columns := []entity.MovementColumnDef{
		{Key: "nomenclature", Label: "Номенклатура", Type: "ref"},
		{Key: "warehouse", Label: "Склад", Type: "ref"},
		{Key: "quantity", Label: "Количество", Type: "quantity"},
	}

	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		data := map[string]interface{}{
			"nomenclature": entity.MovementRefValue{ID: m.NomenclatureID.String(), Name: m.NomenclatureID.String()},
			"warehouse": entity.MovementRefValue{ID: m.WarehouseID.String(), Name: m.WarehouseID.String()},
			"quantity":  m.Quantity.Float64(),
		}

		result = append(result, entity.DocumentMovement{
			RegisterName: s.RegisterName(),
			RecordType:   string(m.RecordType),
			Period:       m.Period,
			Columns:      columns,
			Data:         data,
		})
	}

	return result, nil
}
