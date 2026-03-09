// Package register_repo provides PostgreSQL implementations for register repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context.
package register_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/registers/stock"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	stockMovementsTable = "reg_stock_movements"
	stockBalancesTable  = "reg_stock_balances"
)

// StockRepo implements stock.Repository.
// In Database-per-Tenant architecture, TxManager is obtained from context.
type StockRepo struct {
	builder squirrel.StatementBuilderType
}

// NewStockRepo creates a new stock register repository.
func NewStockRepo() *StockRepo {
	return &StockRepo{
		builder: squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// getTxManager retrieves TxManager from context.
func (r *StockRepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// CreateMovements batch inserts movements.
func (r *StockRepo) CreateMovements(ctx context.Context, movements []entity.StockMovement) error {
	if len(movements) == 0 {
		return nil
	}

	// Fast path: COPY when inside a transaction.
	txm := r.getTxManager(ctx)
	if tx := txm.GetTx(ctx); tx != nil {
		inserter := postgres.NewBatchInserter(txm)
		columns := []string{
			"line_id", "recorder_id", "recorder_type", "recorder_version",
			"period", "record_type",
			"warehouse_id", "product_id", "quantity", "created_at",
		}
		rows := make([][]any, 0, len(movements))
		for _, m := range movements {
			rows = append(rows, []any{
				m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
				m.Period, m.RecordType,
				m.WarehouseID, m.ProductID, m.Quantity, m.CreatedAt,
			})
		}
		if _, err := inserter.CopyFromSlice(ctx, stockMovementsTable, columns, rows); err != nil {
			return fmt.Errorf("copy movements: %w", err)
		}
		return nil
	}

	// Fallback: non-transactional insert (slower). Prefer calling CreateMovements within tx.
	q := r.builder.Insert(stockMovementsTable).Columns(
		"line_id", "recorder_id", "recorder_type", "recorder_version",
		"period", "record_type",
		"warehouse_id", "product_id", "quantity", "created_at",
	)

	for _, m := range movements {
		q = q.Values(
			m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
			m.Period, m.RecordType,
			m.WarehouseID, m.ProductID, m.Quantity, m.CreatedAt,
		)
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	_, err = querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("insert movements: %w", err)
	}

	return nil
}

// DeleteMovementsByRecorder removes movements for a document version.
func (r *StockRepo) DeleteMovementsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	q := r.builder.Delete(stockMovementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		Where(squirrel.Lt{"recorder_version": beforeVersion})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	_, err = querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("delete movements: %w", err)
	}

	return nil
}

// GetMovementsByRecorder retrieves movements for a document.
func (r *StockRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.StockMovement, error) {
	q := r.builder.Select(
		"line_id", "recorder_id", "recorder_type", "recorder_version",
		"period", "record_type",
		"warehouse_id", "product_id", "quantity", "created_at",
	).From(stockMovementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		OrderBy("created_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var movements []entity.StockMovement
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select movements: %w", err)
	}

	return movements, nil
}

// GetBalance returns current balance for warehouse+product.
func (r *StockRepo) GetBalance(ctx context.Context, warehouseID, productID id.ID) (entity.StockBalance, error) {
	var balance entity.StockBalance

	q := r.builder.Select(
		"warehouse_id", "product_id",
		"quantity", "last_movement_at", "updated_at",
	).From(stockBalancesTable).
		Where(squirrel.Eq{
			"warehouse_id": warehouseID,
			"product_id":   productID,
		}).Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return balance, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &balance, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity.StockBalance{
				WarehouseID: warehouseID,
				ProductID:   productID,
				Quantity:    0,
			}, nil
		}
		return balance, fmt.Errorf("get balance: %w", err)
	}

	return balance, nil
}

// GetBalanceForUpdate returns balance with pessimistic lock.
func (r *StockRepo) GetBalanceForUpdate(ctx context.Context, warehouseID, productID id.ID) (entity.StockBalance, error) {
	var balance entity.StockBalance

	sql := `
		SELECT warehouse_id, product_id, quantity, last_movement_at, updated_at
		FROM reg_stock_balances
		WHERE warehouse_id = $1 AND product_id = $2
		FOR UPDATE
	`

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	err := pgxscan.Get(ctx, querier, &balance, sql, warehouseID, productID)
	if err != nil {
		if pgxscan.NotFound(err) {
			return entity.StockBalance{
				WarehouseID: warehouseID,
				ProductID:   productID,
				Quantity:    0,
			}, nil
		}
		return balance, fmt.Errorf("get balance for update: %w", err)
	}

	return balance, nil
}

// GetBalancesByWarehouse returns balances for a warehouse.
func (r *StockRepo) GetBalancesByWarehouse(ctx context.Context, warehouseID id.ID, filter stock.BalanceFilter) ([]entity.StockBalance, error) {
	q := r.builder.Select(
		"warehouse_id", "product_id",
		"quantity", "last_movement_at", "updated_at",
	).From(stockBalancesTable).
		Where(squirrel.Eq{"warehouse_id": warehouseID})

	if filter.ExcludeZero {
		q = q.Where(squirrel.NotEq{"quantity": int64(0)})
	}

	if len(filter.ProductIDs) > 0 {
		q = q.Where(squirrel.Eq{"product_id": filter.ProductIDs})
	}

	if filter.MinQuantity != nil {
		q = q.Where(squirrel.GtOrEq{"quantity": filter.MinQuantity.Int64Scaled()})
	}

	if filter.MaxQuantity != nil {
		q = q.Where(squirrel.LtOrEq{"quantity": filter.MaxQuantity.Int64Scaled()})
	}

	q = q.OrderBy("product_id")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var balances []entity.StockBalance
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &balances, sql, args...); err != nil {
		return nil, fmt.Errorf("select balances: %w", err)
	}

	return balances, nil
}

// GetBalancesByProduct returns balances for a product across warehouses.
func (r *StockRepo) GetBalancesByProduct(ctx context.Context, productID id.ID) ([]entity.StockBalance, error) {
	q := r.builder.Select(
		"warehouse_id", "product_id",
		"quantity", "last_movement_at", "updated_at",
	).From(stockBalancesTable).
		Where(squirrel.Eq{"product_id": productID}).
		Where(squirrel.NotEq{"quantity": int64(0)}).
		OrderBy("warehouse_id")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var balances []entity.StockBalance
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &balances, sql, args...); err != nil {
		return nil, fmt.Errorf("select balances: %w", err)
	}

	return balances, nil
}

// GetBalancesAtDate calculates balance as of a specific date.
func (r *StockRepo) GetBalancesAtDate(ctx context.Context, warehouseID, productID id.ID, date time.Time) (types.Quantity, error) {
	sql := `
		SELECT COALESCE(
			SUM(CASE WHEN record_type = 'receipt' THEN quantity ELSE -quantity END),
			0
		)
		FROM reg_stock_movements
		WHERE warehouse_id = $1 
		  AND product_id = $2 
		  AND period <= $3
	`

	var balanceScaled int64
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	err := querier.QueryRow(ctx, sql, warehouseID, productID, date).Scan(&balanceScaled)
	if err != nil && err != pgx.ErrNoRows {
		return 0, fmt.Errorf("calculate balance at date: %w", err)
	}

	return types.NewQuantityFromInt64Scaled(balanceScaled), nil
}

// GetMovementHistory returns movement history for a product.
func (r *StockRepo) GetMovementHistory(ctx context.Context, productID id.ID, filter stock.MovementFilter) ([]entity.StockMovement, error) {
	q := r.builder.Select(
		"line_id", "recorder_id", "recorder_type", "recorder_version",
		"period", "record_type",
		"warehouse_id", "product_id", "quantity", "created_at",
	).From(stockMovementsTable).
		Where(squirrel.Eq{"product_id": productID})

	if filter.WarehouseID != nil {
		q = q.Where(squirrel.Eq{"warehouse_id": *filter.WarehouseID})
	}

	if filter.RecordType != nil {
		q = q.Where(squirrel.Eq{"record_type": *filter.RecordType})
	}

	if filter.FromDate != nil {
		q = q.Where(squirrel.GtOrEq{"period": *filter.FromDate})
	}

	if filter.ToDate != nil {
		q = q.Where(squirrel.LtOrEq{"period": *filter.ToDate})
	}

	q = q.OrderBy("period DESC", "created_at DESC")

	if filter.Limit > 0 {
		q = q.Limit(uint64(filter.Limit))
	}
	if filter.Offset > 0 {
		q = q.Offset(uint64(filter.Offset))
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var movements []entity.StockMovement
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select history: %w", err)
	}

	return movements, nil
}

// GetTurnover calculates turnover for period.
func (r *StockRepo) GetTurnover(ctx context.Context, filter stock.TurnoverFilter) (stock.Turnover, error) {
	var result stock.Turnover

	args := []any{filter.FromDate, filter.ToDate}
	baseConditions := "period >= $1 AND period < $2"
	argIndex := 3

	if filter.WarehouseID != nil {
		baseConditions += fmt.Sprintf(" AND warehouse_id = $%d", argIndex)
		args = append(args, *filter.WarehouseID)
		result.WarehouseID = *filter.WarehouseID
		argIndex++
	}

	if filter.ProductID != nil {
		baseConditions += fmt.Sprintf(" AND product_id = $%d", argIndex)
		args = append(args, *filter.ProductID)
		result.ProductID = *filter.ProductID
		argIndex++
	}

	sql := fmt.Sprintf(`
		SELECT 
			COALESCE(SUM(CASE WHEN record_type = 'receipt' THEN quantity ELSE 0 END), 0) as receipt,
			COALESCE(SUM(CASE WHEN record_type = 'expense' THEN quantity ELSE 0 END), 0) as expense
		FROM reg_stock_movements
		WHERE %s
	`, baseConditions)

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	var receiptScaled, expenseScaled int64
	err := querier.QueryRow(ctx, sql, args...).Scan(&receiptScaled, &expenseScaled)
	if err != nil && err != pgx.ErrNoRows {
		return result, fmt.Errorf("calculate turnover: %w", err)
	}
	result.Receipt = types.NewQuantityFromInt64Scaled(receiptScaled)
	result.Expense = types.NewQuantityFromInt64Scaled(expenseScaled)

	// Calculate opening balance
	openingArgs := []any{filter.FromDate}
	openingConditions := "period < $1"
	argIndex = 2

	if filter.WarehouseID != nil {
		openingConditions += fmt.Sprintf(" AND warehouse_id = $%d", argIndex)
		openingArgs = append(openingArgs, *filter.WarehouseID)
		argIndex++
	}

	if filter.ProductID != nil {
		openingConditions += fmt.Sprintf(" AND product_id = $%d", argIndex)
		openingArgs = append(openingArgs, *filter.ProductID)
	}

	openingSQL := fmt.Sprintf(`
		SELECT COALESCE(
			SUM(CASE WHEN record_type = 'receipt' THEN quantity ELSE -quantity END),
			0
		)
		FROM reg_stock_movements
		WHERE %s
	`, openingConditions)

	var openingScaled int64
	err = querier.QueryRow(ctx, openingSQL, openingArgs...).Scan(&openingScaled)
	if err != nil && err != pgx.ErrNoRows {
		return result, fmt.Errorf("calculate opening balance: %w", err)
	}
	result.OpeningBalance = types.NewQuantityFromInt64Scaled(openingScaled)

	result.ClosingBalance = result.OpeningBalance + result.Receipt - result.Expense

	return result, nil
}

// RecalculateBalances rebuilds balance table from movements.
func (r *StockRepo) RecalculateBalances(ctx context.Context, warehouseID, productID *id.ID) error {
	// This would call a stored procedure
	// For now, skip implementation
	return nil
}

// CheckStockAvailability checks if required quantity is available.
func (r *StockRepo) CheckStockAvailability(ctx context.Context, warehouseID, productID id.ID, requiredQty types.Quantity) error {
	balance, err := r.GetBalanceForUpdate(ctx, warehouseID, productID)
	if err != nil {
		return fmt.Errorf("get balance: %w", err)
	}

	if balance.Quantity < requiredQty {
		return apperror.NewInsufficientStock(productID.String(), requiredQty.Float64(), balance.Quantity.Float64())
	}

	return nil
}

// Ensure interface compliance.
var _ stock.Repository = (*StockRepo)(nil)
