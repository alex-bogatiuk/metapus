// Package settlement provides the settlement accumulation register.
package settlement

import (
	"context"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// Repository defines operations for the settlement register.
type Repository interface {
	// Movement operations

	// CreateMovements batch inserts movements (used during posting)
	CreateMovements(ctx context.Context, movements []entity.SettlementMovement) error

	// DeleteMovementsByRecorder removes all movements for a document version
	DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error

	// GetMovementsByRecorder retrieves all movements for a document
	GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.SettlementMovement, error)

	// Balance operations

	// GetBalance returns current balance for counterparty+contract+currency
	GetBalance(ctx context.Context, counterpartyID id.ID, contractID *id.ID, currencyID id.ID) (entity.SettlementBalance, error)

	// GetBalancesByCounterparty returns all non-zero balances for a counterparty
	GetBalancesByCounterparty(ctx context.Context, counterpartyID id.ID) ([]entity.SettlementBalance, error)
}
