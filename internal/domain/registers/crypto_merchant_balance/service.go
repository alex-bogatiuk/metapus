// Package crypto_merchant_balance provides the crypto merchant balance accumulation register.
// Tracks platform debt to merchants per token (analogue of reg_settlement for crypto).
// Receipt = platform owes more to merchant (payment received).
// Expense = platform paid merchant (withdrawal processed).
package crypto_merchant_balance

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// Repository defines storage operations for crypto merchant balance movements.
type Repository interface {
	CreateMovements(ctx context.Context, movements []entity.CryptoMerchantBalanceMovement) error
	DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CryptoMerchantBalanceMovement, error)
}

// Service provides business operations for the crypto merchant balance register.
type Service struct {
	repo Repository
}

// NewService creates a new crypto merchant balance register service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// RecordMovements records crypto merchant balance movements from a document posting.
func (s *Service) RecordMovements(ctx context.Context, movements []entity.CryptoMerchantBalanceMovement) error {
	if len(movements) == 0 {
		return nil
	}

	if err := s.repo.CreateMovements(ctx, movements); err != nil {
		return fmt.Errorf("create crypto merchant balance movements: %w", err)
	}

	logger.Info(ctx, "recorded crypto merchant balance movements",
		"count", len(movements),
		"recorder_id", movements[0].RecorderID,
	)
	return nil
}

// ReverseMovements removes movements for a document (used during unposting).
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.DeleteMovementsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("delete crypto merchant balance movements: %w", err)
	}

	logger.Info(ctx, "reversed crypto merchant balance movements",
		"recorder_id", recorderID,
		"before_version", beforeVersion,
	)
	return nil
}

// --- MovementProvider interface ---

func (s *Service) RegisterName() string {
	return "Крипто-расчёты с мерчантами"
}

func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return nil, fmt.Errorf("get crypto merchant balance movements: %w", err)
	}

	columns := []entity.MovementColumnDef{
		{Key: "merchant", Label: "Мерчант", Type: "ref"},
		{Key: "token", Label: "Токен", Type: "ref"},
		{Key: "amount", Label: "Сумма", Type: "text"}, // text — CryptoAmount is string
	}

	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		data := map[string]interface{}{
			"merchant": entity.MovementRefValue{ID: m.MerchantID.String(), Name: m.MerchantID.String()},
			"token":    entity.MovementRefValue{ID: m.TokenID.String(), Name: m.TokenID.String()},
			"amount":   m.Amount.String(),
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
