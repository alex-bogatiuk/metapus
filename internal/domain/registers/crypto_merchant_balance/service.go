// Package crypto_merchant_balance provides the crypto merchant balance accumulation register.
// Tracks platform debt to merchants per token (analogue of reg_settlement for crypto).
// Receipt = platform owes more to merchant (payment received).
// Expense = platform paid merchant (withdrawal processed).
package crypto_merchant_balance

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/pkg/logger"
)

// Repository defines storage operations for crypto merchant balance movements.
type Repository interface {
	CreateMovements(ctx context.Context, movements []entity.CryptoMerchantBalanceMovement) error
	DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CryptoMerchantBalanceMovement, error)
	GetBalancesForUpdate(ctx context.Context, keys []MerchantBalanceKey) ([]MerchantBalanceEntry, error)
}

// MerchantBalanceKey identifies a unique merchant+token balance row for locking.
type MerchantBalanceKey struct {
	MerchantID id.ID
	TokenID    id.ID
}

// MerchantBalanceEntry represents a locked balance row.
type MerchantBalanceEntry struct {
	MerchantID id.ID              `db:"merchant_id"`
	TokenID    id.ID              `db:"token_id"`
	Amount     types.CryptoAmount `db:"amount"`
}

// MerchantBalanceReservation represents a balance check request.
type MerchantBalanceReservation struct {
	MerchantID id.ID
	TokenID    id.ID
	Required   types.CryptoAmount
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

// ReverseMovements removes movements for a document (used during unposting/re-posting).
// Standard PostingEngine behavior — DELETE old movements.
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

// StornoMovements creates compensating (storno) entries for all movements of a document.
// Original movements remain untouched — new movements with inverted record_type are added.
//
// Used for business-level reversals (e.g., WithdrawalRequest rejection), NOT by PostingEngine.
// The DB trigger automatically adjusts balances on INSERT.
//
// Example:
//
//	Original:  expense -1000 USDT (balance reduced)
//	Storno:    receipt +1000 USDT (balance restored)
func (s *Service) StornoMovements(ctx context.Context, recorderID id.ID, stornoVersion int) error {
	originals, err := s.repo.GetMovementsByRecorder(ctx, recorderID)
	if err != nil {
		return fmt.Errorf("get movements for storno: %w", err)
	}
	if len(originals) == 0 {
		return nil
	}

	storno := make([]entity.CryptoMerchantBalanceMovement, 0, len(originals))
	for _, m := range originals {
		// Skip movements that are already storno (prevent double-storno).
		if m.RecorderVersion >= stornoVersion {
			continue
		}

		// Invert record_type: receipt ↔ expense.
		invertedType := entity.RecordTypeExpense
		if m.RecordType == entity.RecordTypeExpense {
			invertedType = entity.RecordTypeReceipt
		}

		storno = append(storno, entity.NewCryptoMerchantBalanceMovement(
			m.RecorderID,
			m.RecorderType,
			stornoVersion,
			time.Now().UTC(),
			invertedType,
			m.MerchantID,
			m.TokenID,
			m.Amount,
		))
	}

	if len(storno) == 0 {
		return nil
	}

	if err := s.repo.CreateMovements(ctx, storno); err != nil {
		return fmt.Errorf("record storno movements: %w", err)
	}

	logger.Info(ctx, "recorded storno movements for crypto merchant balance",
		"recorder_id", recorderID,
		"storno_version", stornoVersion,
		"count", len(storno),
	)
	return nil
}

// CheckAndReserveMerchantBalance validates balance availability for expense movements
// with pessimistic locking in deterministic key order (deadlock-safe).
// Analogous to stock.Service.CheckAndReserveStock.
func (s *Service) CheckAndReserveMerchantBalance(ctx context.Context, items []MerchantBalanceReservation) error {
	if len(items) == 0 {
		return nil
	}

	// Build keys for batch lookup.
	keys := make([]MerchantBalanceKey, len(items))
	for i, item := range items {
		keys[i] = MerchantBalanceKey{
			MerchantID: item.MerchantID,
			TokenID:    item.TokenID,
		}
	}

	// Single DB roundtrip: lock all rows in deterministic order.
	balances, err := s.repo.GetBalancesForUpdate(ctx, keys)
	if err != nil {
		return fmt.Errorf("get balances for update: %w", err)
	}

	// Build lookup map: (merchantID, tokenID) → balance.
	type dimKey struct {
		m, t id.ID
	}
	balanceMap := make(map[dimKey]types.CryptoAmount, len(balances))
	for _, b := range balances {
		balanceMap[dimKey{b.MerchantID, b.TokenID}] = b.Amount
	}

	// Validate each reservation.
	for _, item := range items {
		available := balanceMap[dimKey{item.MerchantID, item.TokenID}]
		if available < item.Required {
			logger.Warn(ctx, "insufficient merchant balance",
				"merchant_id", item.MerchantID,
				"token_id", item.TokenID,
				"available", available,
				"requested", item.Required,
			)
			return apperror.NewValidation("insufficient balance for withdrawal").
				WithDetail("token_id", item.TokenID.String())
		}
	}

	return nil
}

// SortMerchantBalanceKeys sorts keys by MerchantID then TokenID for resource ordering.
// Prevents deadlocks when locking multiple balance rows.
func SortMerchantBalanceKeys(keys []MerchantBalanceKey) {
	sort.Slice(keys, func(i, j int) bool {
		if c := bytes.Compare(keys[i].MerchantID[:], keys[j].MerchantID[:]); c != 0 {
			return c < 0
		}
		return bytes.Compare(keys[i].TokenID[:], keys[j].TokenID[:]) < 0
	})
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
