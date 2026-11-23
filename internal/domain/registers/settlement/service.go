// Package settlement provides the settlement accumulation register service.
package settlement

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

// Service provides business operations for the settlement register.
type Service struct {
	repo Repository
}

// NewService creates a new settlement register service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// RecordMovements records settlement movements from a document posting.
func (s *Service) RecordMovements(ctx context.Context, movements []entity.SettlementMovement) error {
	if len(movements) == 0 {
		return nil
	}

	for i, m := range movements {
		if !m.Amount.IsPositive() {
			return apperror.NewValidation(fmt.Sprintf("settlement movement %d: amount must be positive", i))
		}
		if id.IsNil(m.RecorderID) {
			return apperror.NewValidation(fmt.Sprintf("settlement movement %d: recorder_id is required", i))
		}
	}

	if err := s.repo.CreateMovements(ctx, movements); err != nil {
		return fmt.Errorf("create settlement movements: %w", err)
	}

	logger.Info(ctx, "recorded settlement movements",
		"count", len(movements),
		"recorder_id", movements[0].RecorderID,
	)

	return nil
}

// ReverseMovements removes movements for a document (used during unposting).
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.DeleteMovementsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("delete settlement movements: %w", err)
	}

	logger.Info(ctx, "reversed settlement movements",
		"recorder_id", recorderID,
		"before_version", beforeVersion,
	)

	return nil
}

// ---------------------------------------------------------------------------
// Implementation of entity.MovementProvider
// ---------------------------------------------------------------------------

func (s *Service) RegisterName() string {
	return "Взаиморасчеты с контрагентами"
}

func (s *Service) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return nil, fmt.Errorf("get settlement movements: %w", err)
	}

	columns := []entity.MovementColumnDef{
		{Key: "counterparty", PolicyField: "counterparty_id", Label: "Контрагент", Type: "ref", RefEntity: "Counterparty"},
		{Key: "currency", PolicyField: "currency_id", Label: "Валюта", Type: "ref", RefEntity: "Currency"},
		{Key: "amount", PolicyField: "amount", Label: "Сумма", Type: "amount", CurrencyRef: "currency"},
	}
	// Add contract column only if any movement has a contract
	hasContract := false
	for _, m := range movements {
		if m.ContractID != nil {
			hasContract = true
			break
		}
	}
	if hasContract {
		columns = append(columns, entity.MovementColumnDef{Key: "contract", PolicyField: "contract_id", Label: "Договор", Type: "ref", RefEntity: "Contract"})
	}

	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		data := map[string]any{
			"counterparty": entity.MovementRefValue{ID: m.CounterpartyID.String(), Name: m.CounterpartyID.String()},
			"currency":     entity.MovementRefValue{ID: m.CurrencyID.String(), Name: m.CurrencyID.String()},
			"amount":       m.Amount, // Preserve the monetary type for the HTTP codec.
		}
		if m.ContractID != nil {
			data["contract"] = entity.MovementRefValue{ID: m.ContractID.String(), Name: m.ContractID.String()}
		}

		result = append(result, entity.DocumentMovement{PolicyEntity: "settlement",
			RegisterName: s.RegisterName(),
			RecordType:   string(m.RecordType),
			Period:       m.Period,
			Columns:      columns,
			Data:         data,
		})
	}

	return result, nil
}
