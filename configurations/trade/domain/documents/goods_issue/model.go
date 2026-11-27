// Package goods_issue is the goods issue (Расходная накладная): parts sold
// to a customer from a warehouse. Posting takes them from the warehouse stock
// and from the stock: each line's Reserve from the reserve of the issue's
// customer order, the rest from the free stock. Under an order it also takes
// the shipped quantity from the order. The registers refuse an issue of more
// than the warehouse holds, than is free, than the order reserved or than it
// ordered.
package goods_issue

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/customer_orders"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/inventory_cost"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/sales"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/stock"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/warehouse_stock"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "goods_issue", Name: "GoodsIssue", Route: "goods-issue", Table: "doc_goods_issues",
	Kind: entity.DocumentKind,
	// A document without a number gets the next one of its yearly series (РН-2026-00001).
	Number: entity.NumberRule{Prefix: "РН"},
	Parts:  []entity.Part{{Key: "goods", Field: "goods"}},
	// An issue changed in a closed month moves the cost sequence back.
	Sequence: entity.SequenceRule{In: []string{inventory_cost.Sequence}},
}

// GoodsIssue is one sale of parts to a customer.
type GoodsIssue struct {
	entity.Document
	CustomerID  id.ID `db:"customer_id" json:"customerId" meta:"label:Покупатель,ref:counterparty" binding:"required"`
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" binding:"required"`
	CurrencyID  id.ID `db:"currency_id" json:"currencyId" meta:"label:Валюта,ref:currency" binding:"required"`
	// CustomerOrderID is the order the issue ships, if any.
	CustomerOrderID *id.ID      `db:"customer_order_id" json:"customerOrderId,omitempty" meta:"label:Заказ покупателя,ref:customer_order"`
	Goods           []GoodsLine `db:"-" json:"goods" meta:"label:Товары"`
}

// GoodsLine is a row of the goods table part (doc_goods_issues__goods).
type GoodsLine struct {
	LineID   id.ID          `db:"line_id" json:"lineId"`
	LineNo   int            `db:"line_no" json:"lineNo" meta:"label:№"`
	ItemID   id.ID          `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" binding:"required"`
	Quantity types.Quantity `db:"quantity" json:"quantity" meta:"label:Количество"`
	// Reserve is how much of Quantity comes from the order's reserve.
	Reserve types.Quantity   `db:"reserve" json:"reserve" meta:"label:Из резерва"`
	Price   types.MinorUnits `db:"price" json:"price" meta:"label:Цена"`
	Amount  types.MinorUnits `db:"amount" json:"amount" meta:"label:Сумма"`
}

func (GoodsIssue) EntityDefinition() entity.Definition { return Definition }

func (e *GoodsIssue) GetDocumentType() string { return Definition.Name }

func (e *GoodsIssue) Validate(ctx context.Context) error {
	if err := e.Document.Validate(ctx); err != nil {
		return err
	}
	for _, line := range e.Goods {
		if !line.Quantity.IsPositive() {
			return apperror.NewValidation(fmt.Sprintf("line %d: the quantity must be positive", line.LineNo))
		}
		if line.Reserve.IsNegative() || line.Reserve > line.Quantity {
			return apperror.NewValidation(fmt.Sprintf("line %d: the reserve must be between zero and the quantity", line.LineNo))
		}
		if line.Reserve.IsPositive() && e.CustomerOrderID == nil {
			return apperror.NewValidation(fmt.Sprintf("line %d: a reserve is taken under a customer order", line.LineNo))
		}
		if line.Price.IsNegative() || line.Amount.IsNegative() {
			return apperror.NewValidation(fmt.Sprintf("line %d: the price and amount cannot be negative", line.LineNo))
		}
	}
	return nil
}

// CanPost checks the document before posting; whether the warehouse holds
// the goods is the register's check.
func (e *GoodsIssue) CanPost(ctx context.Context) error {
	if err := e.Document.CanPost(ctx); err != nil {
		return err
	}
	if id.IsNil(e.WarehouseID) {
		return apperror.NewValidation("the warehouse is required")
	}
	if len(e.Goods) == 0 {
		return apperror.NewValidation("a goods issue needs at least one line")
	}
	return e.Validate(ctx)
}

// RegisterMovements takes every line's quantity from the warehouse's stock:
// its reserve from the order's, the rest from the free stock; under an order
// it also takes the quantity from the order. The line is a sale: its
// quantity and revenue, costed by the month closing.
func (e *GoodsIssue) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	order := stock.OrderOf(e.CustomerOrderID)
	for _, line := range e.Goods {
		warehouse_stock.Register.Expense(out, warehouse_stock.Movement{
			WarehouseID: e.WarehouseID, ItemID: line.ItemID, Quantity: line.Quantity,
		})
		reserve := line.Reserve
		if order == stock.Free() {
			reserve = 0 // Validate refuses a reserve without an order
		}
		if reserve.IsPositive() {
			stock.Register.Expense(out, stock.Movement{
				WarehouseID: e.WarehouseID, ItemID: line.ItemID, CustomerOrderID: order, Quantity: reserve,
			})
		}
		if free := line.Quantity - reserve; free.IsPositive() {
			stock.Register.Expense(out, stock.Movement{
				WarehouseID: e.WarehouseID, ItemID: line.ItemID, CustomerOrderID: stock.Free(), Quantity: free,
			})
		}
		if order != stock.Free() {
			customer_orders.Register.Expense(out, customer_orders.Movement{
				CustomerOrderID: order, ItemID: line.ItemID, Quantity: line.Quantity,
			})
		}
		sales.Register.Turnover(out, sales.Movement{
			WarehouseID: e.WarehouseID, ItemID: line.ItemID, CustomerID: e.CustomerID, CurrencyID: e.CurrencyID,
			Quantity: line.Quantity, Revenue: line.Amount,
		})
	}
	return nil
}

// GetLineCount sizes the movement set.
func (e *GoodsIssue) GetLineCount() int { return len(e.Goods) }

var _ accumulation.Source = (*GoodsIssue)(nil)
