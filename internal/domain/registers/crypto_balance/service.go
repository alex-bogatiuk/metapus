// Package crypto_balance provides the crypto balance accumulation register.
// Tracks cryptocurrency amounts for wallets by token.
package crypto_balance

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// Repository defines storage operations for crypto balance movements.
type Repository interface {
	CreateMovements(ctx context.Context, movements []entity.CryptoBalanceMovement) error
	DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CryptoBalanceMovement, error)
}

// Service provides business operations for the crypto balance register.
type Service struct {
	repo Repository
}

// NewService creates a new crypto balance register service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// RecordMovements records crypto balance movements from a document posting.
func (s *Service) RecordMovements(ctx context.Context, movements []entity.CryptoBalanceMovement) error {
	if len(movements) == 0 {
		return nil
	}

	if err := s.repo.CreateMovements(ctx, movements); err != nil {
		return fmt.Errorf("create crypto balance movements: %w", err)
	}

	logger.Info(ctx, "recorded crypto balance movements",
		"count", len(movements),
		"recorder_id", movements[0].RecorderID,
	)
	return nil
}

// ReverseMovements removes movements for a document (used during unposting).
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.DeleteMovementsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("delete crypto balance movements: %w", err)
	}

	logger.Info(ctx, "reversed crypto balance movements",
		"recorder_id", recorderID,
		"before_version", beforeVersion,
	)
	return nil
}

// --- MovementProvider interface ---

func (s *Service) RegisterName() string {
	return "Крипто-балансы"
}

func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return nil, fmt.Errorf("get crypto balance movements: %w", err)
	}

	columns := []entity.MovementColumnDef{
		{Key: "wallet", Label: "Кошелёк", Type: "ref"},
		{Key: "token", Label: "Токен", Type: "ref"},
		{Key: "amount", Label: "Сумма", Type: "text"}, // text — CryptoAmount is string
	}

	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		data := map[string]any{
			"wallet": entity.MovementRefValue{ID: m.WalletID.String(), Name: m.WalletID.String()},
			"token":  entity.MovementRefValue{ID: m.TokenID.String(), Name: m.TokenID.String()},
			"amount": m.Amount.String(),
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
