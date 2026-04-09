package register_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/cost"
)

const (
	costMovementsTable = "reg_cost_movements"
	costBalancesTable  = "reg_cost_balances"
)

// costMovementColumns defines column order for cost movements.
var costMovementColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version",
	"period", "record_type",
	"warehouse_id", "product_id", "currency_id", "quantity", "amount", "created_at",
}

// costMovementRowMapper converts a CostMovement to a flat row.
func costMovementRowMapper(m entity.CostMovement) []any {
	return []any{
		m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
		m.Period, m.RecordType,
		m.WarehouseID, m.ProductID, m.CurrencyID, m.Quantity, m.Amount, m.CreatedAt,
	}
}

// CostRepo implements cost.Repository.
// Embeds BaseAccumulationRepo for generic CreateMovements/DeleteMovementsByRecorder.
type CostRepo struct {
	BaseAccumulationRepo[entity.CostMovement]
}

// NewCostRepo creates a new cost register repository.
func NewCostRepo() *CostRepo {
	return &CostRepo{
		BaseAccumulationRepo: NewBaseAccumulationRepo[entity.CostMovement](
			costMovementsTable,
			costMovementColumns,
			costMovementRowMapper,
		),
	}
}

// GetMovementsByRecorder retrieves movements for a document.
func (r *CostRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CostMovement, error) {
	q := r.Builder().Select(costMovementColumns...).
		From(costMovementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		OrderBy("created_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var movements []entity.CostMovement
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select cost movements: %w", err)
	}

	return movements, nil
}

// GetBalance returns current balance for warehouse+product+currency.
func (r *CostRepo) GetBalance(ctx context.Context, warehouseID, productID, currencyID id.ID) (entity.CostBalance, error) {
	var balance entity.CostBalance

	q := r.Builder().Select(
		"warehouse_id", "product_id", "currency_id",
		"quantity", "amount", "last_movement_at", "updated_at",
	).From(costBalancesTable).
		Where(squirrel.Eq{
			"warehouse_id": warehouseID,
			"product_id":   productID,
			"currency_id":  currencyID,
		}).Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return balance, fmt.Errorf("build query: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &balance, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity.CostBalance{
				WarehouseID: warehouseID,
				ProductID:   productID,
				CurrencyID:  currencyID,
				Quantity:    0,
				Amount:      0,
			}, nil
		}
		return balance, fmt.Errorf("get cost balance: %w", err)
	}

	return balance, nil
}

// GetBalancesByWarehouse returns all non-zero balances for a warehouse.
func (r *CostRepo) GetBalancesByWarehouse(ctx context.Context, warehouseID id.ID) ([]entity.CostBalance, error) {
	q := r.Builder().Select(
		"warehouse_id", "product_id", "currency_id",
		"quantity", "amount", "last_movement_at", "updated_at",
	).From(costBalancesTable).
		Where(squirrel.Eq{"warehouse_id": warehouseID}).
		Where(squirrel.NotEq{"quantity": int64(0)}).
		OrderBy("product_id")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var balances []entity.CostBalance
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &balances, sql, args...); err != nil {
		return nil, fmt.Errorf("select cost balances: %w", err)
	}

	return balances, nil
}

// GetBalancesByProduct returns balances across all warehouses for a product.
func (r *CostRepo) GetBalancesByProduct(ctx context.Context, productID id.ID) ([]entity.CostBalance, error) {
	q := r.Builder().Select(
		"warehouse_id", "product_id", "currency_id",
		"quantity", "amount", "last_movement_at", "updated_at",
	).From(costBalancesTable).
		Where(squirrel.Eq{"product_id": productID}).
		Where(squirrel.NotEq{"quantity": int64(0)}).
		OrderBy("warehouse_id")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var balances []entity.CostBalance
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &balances, sql, args...); err != nil {
		return nil, fmt.Errorf("select cost balances: %w", err)
	}

	return balances, nil
}

// Ensure interface compliance.
var _ cost.Repository = (*CostRepo)(nil)
