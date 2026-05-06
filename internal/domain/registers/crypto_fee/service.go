// Package crypto_fee provides the crypto fee accumulation register.
// Tracks platform fees, network fees, and withdrawal fees by merchant and token.
package crypto_fee

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// Repository defines storage operations for crypto fee movements.
type Repository interface {
	CreateMovements(ctx context.Context, movements []entity.CryptoFeeMovement) error
	DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CryptoFeeMovement, error)
}

// Service provides business operations for the crypto fee register.
type Service struct {
	repo Repository
}

// NewService creates a new crypto fee register service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// RecordMovements records crypto fee movements from a document posting.
func (s *Service) RecordMovements(ctx context.Context, movements []entity.CryptoFeeMovement) error {
	if len(movements) == 0 {
		return nil
	}

	if err := s.repo.CreateMovements(ctx, movements); err != nil {
		return fmt.Errorf("create crypto fee movements: %w", err)
	}

	logger.Info(ctx, "recorded crypto fee movements",
		"count", len(movements),
		"recorder_id", movements[0].RecorderID,
	)
	return nil
}

// ReverseMovements removes movements for a document (used during unposting).
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.DeleteMovementsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("delete crypto fee movements: %w", err)
	}

	logger.Info(ctx, "reversed crypto fee movements",
		"recorder_id", recorderID,
		"before_version", beforeVersion,
	)
	return nil
}

// --- MovementProvider interface ---

var _feeTypeNames = map[entity.FeeType]string{
	entity.FeeTypeProcessing: "Процессинг",
	entity.FeeTypeNetwork:    "Сетевая",
	entity.FeeTypeWithdrawal: "Вывод",
	entity.FeeTypeSweep:      "Sweep",
}

func (s *Service) RegisterName() string {
	return "Крипто-комиссии"
}

func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return nil, fmt.Errorf("get crypto fee movements: %w", err)
	}

	columns := []entity.MovementColumnDef{
		{Key: "merchant", Label: "Мерчант", Type: "ref"},
		{Key: "token", Label: "Токен", Type: "ref"},
		{Key: "feeType", Label: "Тип комиссии", Type: "text"},
		{Key: "amount", Label: "Сумма", Type: "text"},
	}

	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		feeTypeName := _feeTypeNames[m.FeeType]
		if feeTypeName == "" {
			feeTypeName = "Неизвестная"
		}

		data := map[string]interface{}{
			"merchant": entity.MovementRefValue{ID: m.MerchantID.String(), Name: m.MerchantID.String()},
			"token":    entity.MovementRefValue{ID: m.TokenID.String(), Name: m.TokenID.String()},
			"feeType":  feeTypeName,
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
