// Package stock_repo stores the warehouse stock register in PostgreSQL. A
// statement trigger keeps reg_warehouse_stock_balances in step with the
// movements (db/configuration/autoparts).
package stock_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/autoparts/domain/registers/warehouse_stock"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/registers"
)

const (
	movementsTable = "reg_warehouse_stock_movements"
	balancesTable  = "reg_warehouse_stock_balances"
)

var movementColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version",
	"period", "record_type", "warehouse_id", "item_id", "quantity", "created_at",
}

func movementRow(m warehouse_stock.Movement) []any {
	return []any{
		m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
		m.Period, m.RecordType, m.WarehouseID, m.ItemID, m.Quantity, m.CreatedAt,
	}
}

// Repo implements warehouse_stock.Repository.
type Repo struct {
	registers.BaseAccumulationRepo[warehouse_stock.Movement]
}

// New creates the register repository.
func New() *Repo {
	return &Repo{BaseAccumulationRepo: registers.NewBaseAccumulationRepo(movementsTable, movementColumns, movementRow)}
}

// GetMovementsByRecorder returns a document's movements.
func (r *Repo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]warehouse_stock.Movement, error) {
	sql, args, err := r.Builder().Select(movementColumns...).From(movementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).OrderBy("created_at", "line_id").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}
	var movements []warehouse_stock.Movement
	if err := pgxscan.Select(ctx, r.GetTxManager(ctx).GetQuerier(ctx), &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select warehouse stock movements: %w", err)
	}
	return movements, nil
}

// GetBalance returns the stock of item at warehouse; none is zero.
func (r *Repo) GetBalance(ctx context.Context, warehouseID, itemID id.ID) (warehouse_stock.Balance, error) {
	balance := warehouse_stock.Balance{WarehouseID: warehouseID, ItemID: itemID}
	sql, args, err := r.Builder().Select("quantity").From(balancesTable).
		Where(squirrel.Eq{"warehouse_id": warehouseID, "item_id": itemID}).ToSql()
	if err != nil {
		return balance, fmt.Errorf("build query: %w", err)
	}
	err = r.GetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, sql, args...).Scan(&balance.Quantity)
	if errors.Is(err, pgx.ErrNoRows) {
		return balance, nil
	}
	if err != nil {
		return balance, fmt.Errorf("select warehouse stock balance: %w", err)
	}
	return balance, nil
}

var _ warehouse_stock.Repository = (*Repo)(nil)
