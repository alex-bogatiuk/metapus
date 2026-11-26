// Package goods_receipt is the goods receipt (Приходная накладная): parts
// received from a supplier into a warehouse. Posting adds them to the
// warehouse stock and to the stock: free, or reserved for the customer order
// a line was bought for.
package goods_receipt

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/stock"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/warehouse_stock"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "goods_receipt", Name: "GoodsReceipt", Route: "goods-receipt", Table: "doc_goods_receipts",
	Kind: entity.DocumentKind,
	// A document without a number gets the next one of its yearly series (ПН-2026-00001).
	Number: entity.NumberRule{Prefix: "ПН"},
	Parts:  []entity.Part{{Key: "goods", Field: "goods"}},
}

// GoodsReceipt is one delivery from a supplier.
type GoodsReceipt struct {
	entity.Document
	SupplierID  id.ID       `db:"supplier_id" json:"supplierId" meta:"label:Поставщик,ref:counterparty" binding:"required"`
	WarehouseID id.ID       `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" binding:"required"`
	CurrencyID  id.ID       `db:"currency_id" json:"currencyId" meta:"label:Валюта,ref:currency" binding:"required"`
	Goods       []GoodsLine `db:"-" json:"goods" meta:"label:Товары"`
}

// GoodsLine is a row of the goods table part (doc_goods_receipts__goods).
type GoodsLine struct {
	LineID   id.ID            `db:"line_id" json:"lineId"`
	LineNo   int              `db:"line_no" json:"lineNo" meta:"label:№"`
	ItemID   id.ID            `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" binding:"required"`
	Quantity types.Quantity   `db:"quantity" json:"quantity" meta:"label:Количество"`
	Price    types.MinorUnits `db:"price" json:"price" meta:"label:Цена"`
	Amount   types.MinorUnits `db:"amount" json:"amount" meta:"label:Сумма"`
	// CustomerOrderID is the order the line was bought for; it arrives
	// reserved for that order (авторезерв).
	CustomerOrderID *id.ID `db:"customer_order_id" json:"customerOrderId,omitempty" meta:"label:Заказ покупателя,ref:customer_order"`
}

func (GoodsReceipt) EntityDefinition() entity.Definition { return Definition }

func (e *GoodsReceipt) GetDocumentType() string { return Definition.Name }

func (e *GoodsReceipt) Validate(ctx context.Context) error {
	if err := e.Document.Validate(ctx); err != nil {
		return err
	}
	for _, line := range e.Goods {
		if !line.Quantity.IsPositive() {
			return apperror.NewValidation(fmt.Sprintf("line %d: the quantity must be positive", line.LineNo))
		}
		if line.Price.IsNegative() || line.Amount.IsNegative() {
			return apperror.NewValidation(fmt.Sprintf("line %d: the price and amount cannot be negative", line.LineNo))
		}
	}
	return nil
}

// CanPost checks the document before posting.
func (e *GoodsReceipt) CanPost(ctx context.Context) error {
	if err := e.Document.CanPost(ctx); err != nil {
		return err
	}
	if id.IsNil(e.WarehouseID) {
		return apperror.NewValidation("the warehouse is required")
	}
	if len(e.Goods) == 0 {
		return apperror.NewValidation("a goods receipt needs at least one line")
	}
	return e.Validate(ctx)
}

// RegisterMovements adds every line's quantity to the warehouse's stock,
// free or reserved for the line's customer order.
func (e *GoodsReceipt) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	for _, line := range e.Goods {
		warehouse_stock.Register.Receipt(out, warehouse_stock.Movement{
			WarehouseID: e.WarehouseID, ItemID: line.ItemID, Quantity: line.Quantity,
		})
		stock.Register.Receipt(out, stock.Movement{
			WarehouseID: e.WarehouseID, ItemID: line.ItemID, CustomerOrderID: stock.OrderOf(line.CustomerOrderID), Quantity: line.Quantity,
		})
	}
	return nil
}

// GetLineCount sizes the movement set.
func (e *GoodsReceipt) GetLineCount() int { return len(e.Goods) }

var _ accumulation.Source = (*GoodsReceipt)(nil)
