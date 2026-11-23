// Package crypto_fee provides the crypto fee accumulation register.
// Tracks platform fees, network fees, and withdrawal fees by merchant and token.
package crypto_fee

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Repository defines storage operations for crypto fee movements.
type Repository interface {
	CreateMovements(ctx context.Context, movements []CryptoFeeMovement) error
	AppendReversalsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]CryptoFeeMovement, error)
	GetBalancesForUpdate(ctx context.Context, keys []BalanceKey) ([]BalanceEntry, error)
}

// BalanceKey identifies a platform fee balance row for locking.
type BalanceKey struct {
	MerchantID id.ID
	TokenID    id.ID
	FeeType    FeeType
}

// BalanceEntry represents a locked fee balance.
type BalanceEntry struct {
	MerchantID id.ID           `db:"merchant_id"`
	TokenID    id.ID           `db:"token_id"`
	FeeType    FeeType         `db:"fee_type"`
	Balance    types.RawAmount `db:"balance"`
}

// BalanceReservation represents a required fee balance amount.
type BalanceReservation struct {
	MerchantID id.ID
	TokenID    id.ID
	FeeType    FeeType
	Required   types.RawAmount
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
func (s *Service) RecordMovements(ctx context.Context, movements []CryptoFeeMovement) error {
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

// ReverseMovements appends opposite movements without deleting financial history.
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.AppendReversalsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("reverse crypto fee movements: %w", err)
	}

	logger.Info(ctx, "reversed crypto fee movements",
		"recorder_id", recorderID,
		"before_version", beforeVersion,
	)
	return nil
}

// CheckAndReserveCryptoFee validates platform revenue availability for expense
// movements with pessimistic locking. The name mirrors other register services:
// it checks under lock, while explicit reservation rows are managed by workers.
func (s *Service) CheckAndReserveCryptoFee(ctx context.Context, items []BalanceReservation) error {
	if len(items) == 0 {
		return nil
	}

	keys := make([]BalanceKey, len(items))
	for i, item := range items {
		keys[i] = BalanceKey{
			MerchantID: item.MerchantID,
			TokenID:    item.TokenID,
			FeeType:    item.FeeType,
		}
	}

	balances, err := s.repo.GetBalancesForUpdate(ctx, keys)
	if err != nil {
		return fmt.Errorf("get crypto fee balances for update: %w", err)
	}

	type dimKey struct {
		m, t id.ID
		ft   FeeType
	}
	balanceMap := make(map[dimKey]types.RawAmount, len(balances))
	for _, b := range balances {
		balanceMap[dimKey{b.MerchantID, b.TokenID, b.FeeType}] = b.Balance
	}

	for _, item := range items {
		available := balanceMap[dimKey{item.MerchantID, item.TokenID, item.FeeType}]
		if available.Cmp(item.Required) < 0 {
			logger.Warn(ctx, "insufficient crypto fee balance",
				"merchant_id", item.MerchantID,
				"token_id", item.TokenID,
				"fee_type", item.FeeType,
				"available", available,
				"requested", item.Required,
			)
			return apperror.NewValidation("insufficient crypto platform revenue").
				WithDetail("merchant_id", item.MerchantID.String()).
				WithDetail("token_id", item.TokenID.String()).
				WithDetail("fee_type", string(item.FeeType))
		}
	}
	return nil
}

// --- MovementProvider interface ---

var _feeTypeNames = map[FeeType]string{
	FeeTypeProcessing: "Процессинг",
	FeeTypeNetwork:    "Сетевая",
	FeeTypeWithdrawal: "Вывод",
	FeeTypeSweep:      "Sweep",
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
		{Key: "merchant", PolicyField: "merchant_id", Label: "Мерчант", Type: "ref", RefEntity: "Merchant"},
		{Key: "token", PolicyField: "token_id", Label: "Токен", Type: "ref", RefEntity: "Token"},
		{Key: "feeType", PolicyField: "fee_type", Label: "Тип комиссии", Type: "text"},
		{Key: "amount", PolicyField: "amount", Label: "Сумма", Type: "text"},
	}

	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		feeTypeName := _feeTypeNames[m.FeeType]
		if feeTypeName == "" {
			feeTypeName = "Неизвестная"
		}

		data := map[string]any{
			"merchant": entity.MovementRefValue{ID: m.MerchantID.String(), Name: m.MerchantID.String()},
			"token":    entity.MovementRefValue{ID: m.TokenID.String(), Name: m.TokenID.String()},
			"feeType":  feeTypeName,
			"amount":   m.Amount.String(),
		}

		result = append(result, entity.DocumentMovement{PolicyEntity: "crypto_fee",
			RegisterName: s.RegisterName(),
			RecordType:   string(m.RecordType),
			Period:       m.Period,
			Columns:      columns,
			Data:         data,
		})
	}

	return result, nil
}
