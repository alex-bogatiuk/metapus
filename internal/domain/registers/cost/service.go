// Package cost provides the cost accumulation register service.
package cost

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// Service provides business operations for the cost register.
type Service struct {
	repo Repository
}

// NewService creates a new cost register service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// RecordMovements records cost movements from a document posting.
func (s *Service) RecordMovements(ctx context.Context, movements []entity.CostMovement) error {
	if len(movements) == 0 {
		return nil
	}

	for i, m := range movements {
		if !m.Quantity.IsPositive() {
			return apperror.NewValidation(fmt.Sprintf("cost movement %d: quantity must be positive", i))
		}
		if !m.Amount.IsPositive() {
			return apperror.NewValidation(fmt.Sprintf("cost movement %d: amount must be positive", i))
		}
		if id.IsNil(m.RecorderID) {
			return apperror.NewValidation(fmt.Sprintf("cost movement %d: recorder_id is required", i))
		}
	}

	if err := s.repo.CreateMovements(ctx, movements); err != nil {
		return fmt.Errorf("create cost movements: %w", err)
	}

	logger.Info(ctx, "recorded cost movements",
		"count", len(movements),
		"recorder_id", movements[0].RecorderID,
	)

	return nil
}

// ReverseMovements removes movements for a document (used during unposting).
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.DeleteMovementsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("delete cost movements: %w", err)
	}

	logger.Info(ctx, "reversed cost movements",
		"recorder_id", recorderID,
		"before_version", beforeVersion,
	)

	return nil
}

// ---------------------------------------------------------------------------
// Implementation of entity.MovementProvider
// ---------------------------------------------------------------------------

func (s *Service) RegisterName() string {
	return "Себестоимость товаров"
}

func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return nil, fmt.Errorf("get cost movements: %w", err)
	}

	columns := []entity.MovementColumnDef{
		{Key: "product", Label: "Товар", Type: "ref"},
		{Key: "warehouse", Label: "Склад", Type: "ref"},
		{Key: "currency", Label: "Валюта", Type: "ref"},
		{Key: "quantity", Label: "Количество", Type: "quantity"},
		{Key: "amount", Label: "Сумма", Type: "amount"},
	}

	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		data := map[string]interface{}{
			"product":   entity.MovementRefValue{ID: m.ProductID.String(), Name: m.ProductID.String()},
			"warehouse": entity.MovementRefValue{ID: m.WarehouseID.String(), Name: m.WarehouseID.String()},
			"currency":  entity.MovementRefValue{ID: m.CurrencyID.String(), Name: m.CurrencyID.String()},
			"quantity":  m.Quantity.Float64(),
			"amount":    int64(m.Amount), // Raw MinorUnits — frontend formats using currency decimalPlaces
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

