package crypto_receipt_suspense

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

type Repository interface {
	CreateMovements(context.Context, []entity.CryptoReceiptSuspenseMovement) error
	GetMovementsByRecorder(context.Context, id.ID) ([]entity.CryptoReceiptSuspenseMovement, error)
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
