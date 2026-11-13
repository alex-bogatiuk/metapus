package crypto_receipt_suspense

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

type Repository interface {
	CreateMovements(context.Context, []entity.CryptoReceiptSuspenseMovement) error
	GetMovementsByRecorder(context.Context, id.ID) ([]entity.CryptoReceiptSuspenseMovement, error)
	// ListRecorderMovements returns every line of one recorder for display.
	ListRecorderMovements(context.Context, id.ID) ([]entity.CryptoReceiptSuspenseMovement, error)
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) RecordMovements(ctx context.Context, movements []entity.CryptoReceiptSuspenseMovement) error {
	return s.repo.CreateMovements(ctx, movements)
}

func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, _ int) error {
	rows, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return err
	}
	if len(rows) != 0 {
		return apperror.NewConflict("receipt suspense requires explicit compensation")
	}
	return nil
}

// --- MovementProvider interface ---

func (s *Service) RegisterName() string { return "Невыясненные поступления" }

// GetDocumentMovements shows how a document moved the unattributed remainder of
// a receipt: the receipt adds it, attributions consume it, stornos return it.
func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.ListRecorderMovements(ctx, recorderID)
	if err != nil {
		return nil, fmt.Errorf("get receipt suspense movements: %w", err)
	}
	columns := []entity.MovementColumnDef{
		{Key: "receipt", PolicyField: "receipt_id", Label: "Поступление", Type: "ref"},
		{Key: "token", PolicyField: "token_id", Label: "Токен", Type: "ref"},
		{Key: "amount", PolicyField: "amount", Label: "Сумма", Type: "text"},
	}
	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		result = append(result, entity.DocumentMovement{PolicyEntity: "crypto_receipt_suspense",
			RegisterName: s.RegisterName(), RecordType: string(m.RecordType), Period: m.Period, Columns: columns,
			Data: map[string]any{
				"receipt": entity.MovementRefValue{ID: m.ReceiptID.String(), Name: m.ReceiptID.String()},
				"token":   entity.MovementRefValue{ID: m.TokenID.String(), Name: m.TokenID.String()},
				"amount":  m.Amount.String(),
			}})
	}
	return result, nil
}
