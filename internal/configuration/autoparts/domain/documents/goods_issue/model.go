// Package goods_issue is the goods issue (Расходная накладная): parts sold
// to a customer from a warehouse. Posting takes them from the warehouse stock
// register, which refuses an issue of more than the warehouse holds.
package goods_issue

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/warehouse_stock"
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
}

// GoodsIssue is one sale of parts to a customer.
type GoodsIssue struct {
	entity.Document
	CustomerID  id.ID       `db:"customer_id" json:"customerId" meta:"label:Покупатель,ref:counterparty" binding:"required"`
	WarehouseID id.ID       `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" binding:"required"`
	CurrencyID  id.ID       `db:"currency_id" json:"currencyId" meta:"label:Валюта,ref:currency" binding:"required"`
	Goods       []GoodsLine `db:"-" json:"goods" meta:"label:Товары"`
}

// GoodsLine is a row of the goods table part (doc_goods_issues__goods).
type GoodsLine struct {
	LineID   id.ID            `db:"line_id" json:"lineId"`
	LineNo   int              `db:"line_no" json:"lineNo" meta:"label:№"`
	ItemID   id.ID            `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" binding:"required"`
	Quantity types.Quantity   `db:"quantity" json:"quantity" meta:"label:Количество"`
	Price    types.MinorUnits `db:"price" json:"price" meta:"label:Цена"`
	Amount   types.MinorUnits `db:"amount" json:"amount" meta:"label:Сумма"`
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

// RegisterMovements takes every line's quantity from the warehouse's stock.
func (e *GoodsIssue) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	for _, line := range e.Goods {
		warehouse_stock.Register.Expense(out, warehouse_stock.Movement{
			WarehouseID: e.WarehouseID, ItemID: line.ItemID, Quantity: line.Quantity,
		})
	}
	return nil
}

// GetLineCount sizes the movement set.
func (e *GoodsIssue) GetLineCount() int { return len(e.Goods) }

var _ accumulation.Source = (*GoodsIssue)(nil)
