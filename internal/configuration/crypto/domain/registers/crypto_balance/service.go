// Package crypto_balance provides the crypto balance accumulation register.
// Tracks cryptocurrency amounts for wallets by token.
package crypto_balance

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/pkg/logger"
)

// Repository defines storage operations for crypto balance movements.
type Repository interface {
	CreateMovements(ctx context.Context, movements []CryptoBalanceMovement) error
	AppendReversalsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]CryptoBalanceMovement, error)
	GetBalancesForUpdate(ctx context.Context, keys []BalanceKey) ([]BalanceEntry, error)
}

// ResourceLocker is implemented by transactional storage adapters. The complete
// old/new wallet set must precede balance validation and reversal allocation.
type ResourceLocker interface {
	LockPostingResources(context.Context, id.ID, []CryptoBalanceMovement) error
}

func (s *Service) LockPostingResources(ctx context.Context, recorderID id.ID, movements []CryptoBalanceMovement) error {
	if locker, ok := s.repo.(ResourceLocker); ok {
		if err := locker.LockPostingResources(ctx, recorderID, movements); err != nil {
			return fmt.Errorf("lock crypto posting resources: %w", err)
		}
	}
	return nil
}

// BalanceKey identifies a unique wallet+token balance row for locking.
type BalanceKey struct {
	WalletID id.ID
	TokenID  id.ID
}

// BalanceEntry represents a locked balance row.
type BalanceEntry struct {
	WalletID id.ID           `db:"wallet_id"`
	TokenID  id.ID           `db:"token_id"`
	Amount   types.RawAmount `db:"amount"`
}

// BalanceReservation represents a crypto wallet balance check request.
type BalanceReservation struct {
	WalletID id.ID
	TokenID  id.ID
	Required types.RawAmount
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
func (s *Service) RecordMovements(ctx context.Context, movements []CryptoBalanceMovement) error {
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

// ReverseMovements appends opposite movements without deleting financial history.
func (s *Service) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	if err := s.repo.AppendReversalsByRecorder(ctx, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("reverse crypto balance movements: %w", err)
	}

	logger.Info(ctx, "reversed crypto balance movements",
		"recorder_id", recorderID,
		"before_version", beforeVersion,
	)
	return nil
}

// CheckAndReserveCryptoBalance validates wallet balance availability for expense
// movements with pessimistic locking in deterministic key order.
func (s *Service) CheckAndReserveCryptoBalance(ctx context.Context, items []BalanceReservation) error {
	if len(items) == 0 {
		return nil
	}

	keys := make([]BalanceKey, len(items))
	for i, item := range items {
		keys[i] = BalanceKey{
			WalletID: item.WalletID,
			TokenID:  item.TokenID,
		}
	}

	balances, err := s.repo.GetBalancesForUpdate(ctx, keys)
	if err != nil {
		return fmt.Errorf("get crypto balances for update: %w", err)
	}

	type dimKey struct {
		w, t id.ID
	}
	balanceMap := make(map[dimKey]types.RawAmount, len(balances))
	for _, b := range balances {
		balanceMap[dimKey{b.WalletID, b.TokenID}] = b.Amount
	}

	for _, item := range items {
		available := balanceMap[dimKey{item.WalletID, item.TokenID}]
		if available.Cmp(item.Required) < 0 {
			return apperror.NewValidation("insufficient crypto wallet balance").
				WithDetail("wallet_id", item.WalletID.String()).
				WithDetail("token_id", item.TokenID.String())
		}
	}

	return nil
}

// SortBalanceKeys sorts keys by WalletID then TokenID for resource ordering.
func SortBalanceKeys(keys []BalanceKey) {
	sort.Slice(keys, func(i, j int) bool {
		if c := bytes.Compare(keys[i].WalletID[:], keys[j].WalletID[:]); c != 0 {
			return c < 0
		}
		return bytes.Compare(keys[i].TokenID[:], keys[j].TokenID[:]) < 0
	})
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
		{Key: "wallet", PolicyField: "wallet_id", Label: "Кошелёк", Type: "ref", RefEntity: "Wallet"},
		{Key: "token", PolicyField: "token_id", Label: "Токен", Type: "ref", RefEntity: "Token"},
		{Key: "amount", PolicyField: "amount", Label: "Сумма", Type: "text"}, // text — RawAmount is string
	}

	result := make([]entity.DocumentMovement, 0, len(movements))
	for _, m := range movements {
		data := map[string]any{
			"wallet": entity.MovementRefValue{ID: m.WalletID.String(), Name: m.WalletID.String()},
			"token":  entity.MovementRefValue{ID: m.TokenID.String(), Name: m.TokenID.String()},
			"amount": m.Amount.String(),
		}

		result = append(result, entity.DocumentMovement{PolicyEntity: "crypto_balance",
			RegisterName: s.RegisterName(),
			RecordType:   string(m.RecordType),
			Period:       m.Period,
			Columns:      columns,
			Data:         data,
		})
	}

	return result, nil
}
