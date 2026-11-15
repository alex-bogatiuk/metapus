// Package crypto_provider_balance provides the external provider prepayment register.
package crypto_provider_balance

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

type Repository interface {
	CreateMovements(ctx context.Context, movements []CryptoProviderBalanceMovement) error
	AppendReversalsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]CryptoProviderBalanceMovement, error)
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) RegisterName() string { return "Авансы крипто-провайдерам" }

func (s *Service) RecordMovements(ctx context.Context, movements []CryptoProviderBalanceMovement) error {
	if len(movements) == 0 {
		return nil
	}
	if err := s.repo.CreateMovements(ctx, movements); err != nil {
		return fmt.Errorf("create crypto provider balance movements: %w", err)
	}
	return nil
}

func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return s.repo.AppendReversalsByRecorder(ctx, recorderID, beforeVersion)
}

func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return nil, err
	}
	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		result = append(result, entity.DocumentMovement{PolicyEntity: "crypto_provider_balance",
			RegisterName: "Авансы крипто-провайдерам", RecordType: string(m.RecordType), Period: m.Period,
			Columns: []entity.MovementColumnDef{
				{Key: "providerAccount", PolicyField: "provider_account_id", Label: "Аккаунт провайдера", Type: "ref"},
				{Key: "token", PolicyField: "token_id", Label: "Токен", Type: "ref"},
				{Key: "operationType", PolicyField: "operation_type", Label: "Операция", Type: "text"},
				{Key: "externalRef", PolicyField: "external_ref", Label: "Внешнее основание", Type: "text"},
				{Key: "amount", PolicyField: "amount", Label: "Сумма", Type: "text"},
			},
			Data: map[string]any{
				"providerAccount": entity.MovementRefValue{ID: m.ProviderAccountID.String(), Name: m.ProviderAccountID.String()},
				"token":           entity.MovementRefValue{ID: m.TokenID.String(), Name: m.TokenID.String()},
				"operationType":   m.OperationType, "externalRef": m.ExternalRef, "amount": m.Amount.String(),
			},
		})
	}
	return result, nil
}
