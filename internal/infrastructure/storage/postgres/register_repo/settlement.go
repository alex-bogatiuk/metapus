package register_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/settlement"
)

const (
	settlementMovementsTable = "reg_settlement_movements"
	settlementBalancesTable  = "reg_settlement_balances"
)

// settlementMovementColumns defines column order for settlement movements.
var settlementMovementColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version",
	"period", "record_type",
	"counterparty_id", "contract_id", "currency_id", "amount", "created_at",
}

// settlementMovementRowMapper converts a SettlementMovement to a flat row.
func settlementMovementRowMapper(m entity.SettlementMovement) []any {
	return []any{
		m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
		m.Period, m.RecordType,
		m.CounterpartyID, m.ContractID, m.CurrencyID, m.Amount, m.CreatedAt,
	}
}

// SettlementRepo implements settlement.Repository.
// Embeds BaseAccumulationRepo for generic CreateMovements/DeleteMovementsByRecorder.
type SettlementRepo struct {
	BaseAccumulationRepo[entity.SettlementMovement]
}

// NewSettlementRepo creates a new settlement register repository.
func NewSettlementRepo() *SettlementRepo {
	return &SettlementRepo{
		BaseAccumulationRepo: NewBaseAccumulationRepo[entity.SettlementMovement](
			settlementMovementsTable,
			settlementMovementColumns,
			settlementMovementRowMapper,
		),
	}
}

// GetMovementsByRecorder retrieves movements for a document.
func (r *SettlementRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.SettlementMovement, error) {
	q := r.Builder().Select(settlementMovementColumns...).
		From(settlementMovementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		OrderBy("created_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var movements []entity.SettlementMovement
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select settlement movements: %w", err)
	}

	return movements, nil
}

// GetBalance returns current balance for counterparty+contract+currency.
func (r *SettlementRepo) GetBalance(ctx context.Context, counterpartyID id.ID, contractID *id.ID, currencyID id.ID) (entity.SettlementBalance, error) {
	var balance entity.SettlementBalance

	q := r.Builder().Select(
		"counterparty_id", "contract_id", "currency_id",
		"amount", "last_movement_at", "updated_at",
	).From(settlementBalancesTable).
		Where(squirrel.Eq{
			"counterparty_id": counterpartyID,
			"currency_id":     currencyID,
		})

	if contractID != nil {
		q = q.Where(squirrel.Eq{"contract_id": *contractID})
	} else {
		q = q.Where("contract_id IS NULL")
	}

	q = q.Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return balance, fmt.Errorf("build query: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &balance, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity.SettlementBalance{
				CounterpartyID: counterpartyID,
				ContractID:     contractID,
				CurrencyID:     currencyID,
				Amount:         0,
			}, nil
		}
		return balance, fmt.Errorf("get settlement balance: %w", err)
	}

	return balance, nil
}

// GetBalancesByCounterparty returns all non-zero balances for a counterparty.
func (r *SettlementRepo) GetBalancesByCounterparty(ctx context.Context, counterpartyID id.ID) ([]entity.SettlementBalance, error) {
	q := r.Builder().Select(
		"counterparty_id", "contract_id", "currency_id",
		"amount", "last_movement_at", "updated_at",
	).From(settlementBalancesTable).
		Where(squirrel.Eq{"counterparty_id": counterpartyID}).
		Where(squirrel.NotEq{"amount": int64(0)}).
		OrderBy("currency_id", "contract_id")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var balances []entity.SettlementBalance
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &balances, sql, args...); err != nil {
		return nil, fmt.Errorf("select settlement balances: %w", err)
	}

	return balances, nil
}

// Ensure interface compliance.
var _ settlement.Repository = (*SettlementRepo)(nil)
