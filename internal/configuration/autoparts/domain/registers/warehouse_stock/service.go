package warehouse_stock

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// Name is the register's key: its movements' policy entity and its key in a
// posting movement set.
const Name = "warehouse_stock"

// Repository stores the register's movements; the balances follow them.
type Repository interface {
	CreateMovements(ctx context.Context, movements []Movement) error
	DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]Movement, error)
	GetBalance(ctx context.Context, warehouseID, itemID id.ID) (Balance, error)
}

// Service records the register's movements and exposes them to the
// document's movements view (entity.MovementProvider).
type Service struct {
	repo Repository
}

// NewService creates the register service.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

// RecordMovements records the movements of a document posting.
func (s *Service) RecordMovements(ctx context.Context, movements []Movement) error {
	if len(movements) == 0 {
		return nil
	}
	if err := s.repo.CreateMovements(ctx, movements); err != nil {
		return fmt.Errorf("create warehouse stock movements: %w", err)
	}
	return nil
}

// ReverseMovements removes the movements of a document's earlier versions.
// The register holds quantities only, so it keeps no reversal history.
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.DeleteMovementsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("reverse warehouse stock movements: %w", err)
	}
	return nil
}

// Balance returns the stock of item at warehouse.
func (s *Service) Balance(ctx context.Context, warehouseID, itemID id.ID) (Balance, error) {
	return s.repo.GetBalance(ctx, warehouseID, itemID)
}

func (s *Service) RegisterName() string { return "Запасы на складах" }

func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return nil, fmt.Errorf("get warehouse stock movements: %w", err)
	}
	columns := []entity.MovementColumnDef{
		{Key: "warehouse", PolicyField: "warehouse_id", Label: "Склад", Type: "ref", RefEntity: "Warehouse"},
		{Key: "item", PolicyField: "item_id", Label: "Номенклатура", Type: "ref", RefEntity: "Item"},
		{Key: "quantity", PolicyField: "quantity", Label: "Количество", Type: "quantity"},
	}
	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		result = append(result, entity.DocumentMovement{
			PolicyEntity: Name,
			RegisterName: s.RegisterName(),
			RecordType:   string(m.RecordType),
			Period:       m.Period,
			Columns:      columns,
			Data: map[string]any{
				"warehouse": entity.MovementRefValue{ID: m.WarehouseID.String(), Name: m.WarehouseID.String()},
				"item":      entity.MovementRefValue{ID: m.ItemID.String(), Name: m.ItemID.String()},
				"quantity":  m.Quantity,
			},
		})
	}
	return result, nil
}

var _ entity.MovementProvider = (*Service)(nil)
