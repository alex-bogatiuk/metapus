// Package crypto_platform_cost provides the crypto platform cost accumulation register.
package crypto_platform_cost

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// Repository defines storage operations for platform cost movements.
type Repository interface {
	CreateMovements(ctx context.Context, movements []entity.CryptoPlatformCostMovement) error
	AppendReversalsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CryptoPlatformCostMovement, error)
}

// Service provides business operations for the crypto platform cost register.
type Service struct {
	repo Repository
}

// NewService creates a new crypto platform cost register service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// RecordMovements records platform cost movements from a document posting.
func (s *Service) RecordMovements(ctx context.Context, movements []entity.CryptoPlatformCostMovement) error {
	if len(movements) == 0 {
		return nil
	}
	if err := s.repo.CreateMovements(ctx, movements); err != nil {
		return fmt.Errorf("create crypto platform cost movements: %w", err)
	}
	logger.Info(ctx, "recorded crypto platform cost movements",
		"count", len(movements),
		"recorder_id", movements[0].RecorderID,
	)
	return nil
}

// ReverseMovements appends opposite movements without deleting financial history.
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.AppendReversalsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("reverse crypto platform cost movements: %w", err)
	}
	logger.Info(ctx, "reversed crypto platform cost movements",
		"recorder_id", recorderID,
		"before_version", beforeVersion,
	)
	return nil
}

// RegisterName returns the human-readable register name.
func (s *Service) RegisterName() string {
	return "Крипто-расходы платформы"
}

// GetDocumentMovements maps platform cost movements to the generic UI movement shape.
func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return nil, fmt.Errorf("get crypto platform cost movements: %w", err)
	}

	columns := []entity.MovementColumnDef{
		{Key: "network", PolicyField: "network_id", Label: "Сеть", Type: "ref"},
		{Key: "operationToken", PolicyField: "operation_token_id", Label: "Токен операции", Type: "ref"},
		{Key: "costToken", PolicyField: "cost_token_id", Label: "Токен расхода", Type: "ref"},
		{Key: "costType", PolicyField: "cost_type", Label: "Тип расхода", Type: "text"},
		{Key: "purpose", PolicyField: "purpose", Label: "Назначение", Type: "text"},
		{Key: "provider", PolicyField: "provider", Label: "Провайдер", Type: "text"},
		{Key: "externalOrderId", PolicyField: "external_order_id", Label: "Внешний ордер", Type: "text"},
		{Key: "amount", PolicyField: "amount", Label: "Сумма", Type: "text"},
	}

	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		var operationToken any
		if m.OperationTokenID != nil {
			operationToken = entity.MovementRefValue{ID: m.OperationTokenID.String(), Name: m.OperationTokenID.String()}
		}
		result = append(result, entity.DocumentMovement{PolicyEntity: "crypto_platform_cost",
			RegisterName: s.RegisterName(),
			RecordType:   string(m.RecordType),
			Period:       m.Period,
			Columns:      columns,
			Data: map[string]any{
				"network":         entity.MovementRefValue{ID: m.NetworkID.String(), Name: m.NetworkID.String()},
				"operationToken":  operationToken,
				"costToken":       entity.MovementRefValue{ID: m.CostTokenID.String(), Name: m.CostTokenID.String()},
				"costType":        string(m.CostType),
				"purpose":         string(m.Purpose),
				"provider":        m.Provider,
				"externalOrderId": m.ExternalOrderID,
				"amount":          m.Amount.String(),
			},
		})
	}
	return result, nil
}
