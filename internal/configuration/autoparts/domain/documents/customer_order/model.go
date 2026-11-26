// Package customer_order is the customer order (Заказ покупателя): parts a
// customer ordered, with the quantity of each line reserved for the order
// from the warehouse's free stock. The rest is bought for the order: a goods
// receipt under the order reserves it on arrival. A goods issue under the
// order ships from the reserve and closes the order.
package customer_order

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/customer_orders"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/stock"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "customer_order", Name: "CustomerOrder", Route: "customer-order", Table: "doc_customer_orders",
	Kind: entity.DocumentKind,
	// A document without a number gets the next one of its yearly series (ЗП-2026-00001).
	Number: entity.NumberRule{Prefix: "ЗП"},
	Parts:  []entity.Part{{Key: "goods", Field: "goods"}},
}

// CustomerOrder is one order of a customer.
type CustomerOrder struct {
	entity.Document
	CustomerID  id.ID       `db:"customer_id" json:"customerId" meta:"label:Покупатель,ref:counterparty" binding:"required"`
	WarehouseID id.ID       `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" binding:"required"`
	CurrencyID  id.ID       `db:"currency_id" json:"currencyId" meta:"label:Валюта,ref:currency" binding:"required"`
	Goods       []GoodsLine `db:"-" json:"goods" meta:"label:Товары"`
}

// GoodsLine is a row of the goods table part (doc_customer_orders__goods).
// Reserve is how much of Quantity the order takes from the free stock now.
type GoodsLine struct {
	LineID   id.ID            `db:"line_id" json:"lineId"`
	LineNo   int              `db:"line_no" json:"lineNo" meta:"label:№"`
	ItemID   id.ID            `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" binding:"required"`
	Quantity types.Quantity   `db:"quantity" json:"quantity" meta:"label:Количество"`
	Reserve  types.Quantity   `db:"reserve" json:"reserve" meta:"label:Резерв"`
	Price    types.MinorUnits `db:"price" json:"price" meta:"label:Цена"`
	Amount   types.MinorUnits `db:"amount" json:"amount" meta:"label:Сумма"`
}

func (CustomerOrder) EntityDefinition() entity.Definition { return Definition }

func (e *CustomerOrder) GetDocumentType() string { return Definition.Name }

func (e *CustomerOrder) Validate(ctx context.Context) error {
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
		if line.Price.IsNegative() || line.Amount.IsNegative() {
			return apperror.NewValidation(fmt.Sprintf("line %d: the price and amount cannot be negative", line.LineNo))
		}
	}
	return nil
}

// CanPost checks the document before posting; whether the free stock holds
// the reserve is the stock register's check.
func (e *CustomerOrder) CanPost(ctx context.Context) error {
	if err := e.Document.CanPost(ctx); err != nil {
		return err
	}
	if id.IsNil(e.WarehouseID) {
		return apperror.NewValidation("the warehouse is required")
	}
	if len(e.Goods) == 0 {
		return apperror.NewValidation("a customer order needs at least one line")
	}
	return e.Validate(ctx)
}

// RegisterMovements adds every line to the customer's orders and moves its
// reserve from the free stock to the order.
func (e *CustomerOrder) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	for _, line := range e.Goods {
		customer_orders.Register.Receipt(out, customer_orders.Movement{
			CustomerOrderID: e.ID, ItemID: line.ItemID, Quantity: line.Quantity,
		})
		if !line.Reserve.IsPositive() {
			continue
		}
		stock.Register.Expense(out, stock.Movement{
			WarehouseID: e.WarehouseID, ItemID: line.ItemID, CustomerOrderID: stock.Free(), Quantity: line.Reserve,
		})
		stock.Register.Receipt(out, stock.Movement{
			WarehouseID: e.WarehouseID, ItemID: line.ItemID, CustomerOrderID: e.ID, Quantity: line.Reserve,
		})
	}
	return nil
}

// GetLineCount sizes the movement set.
func (e *CustomerOrder) GetLineCount() int { return len(e.Goods) }

var _ accumulation.Source = (*CustomerOrder)(nil)
