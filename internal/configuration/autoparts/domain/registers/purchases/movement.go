// Package purchases is the turnover register of purchases (Закупки):
// quantities and amounts bought, by warehouse, item, supplier and currency.
// The goods receipt writes it; the month closing takes the month's
// purchases into the cost of the stock.
package purchases

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Movement is one purchase: a line of a goods receipt.
type Movement struct {
	entity.MovementBase

	WarehouseID id.ID            `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" register:"dimension"`
	ItemID      id.ID            `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" register:"dimension"`
	SupplierID  id.ID            `db:"supplier_id" json:"supplierId" meta:"label:Поставщик,ref:counterparty" register:"dimension"`
	CurrencyID  id.ID            `db:"currency_id" json:"currencyId" meta:"label:Валюта,ref:currency" register:"dimension"`
	Quantity    types.Quantity   `db:"quantity" json:"quantity" meta:"label:Количество" register:"resource"`
	Amount      types.MinorUnits `db:"amount" json:"amount" meta:"label:Сумма" register:"resource,currency=currency_id"`
}

// Register is the purchases register (reg_purchases_movements).
var Register = accumulation.New[Movement](accumulation.Definition{
	Key: "purchases", Label: "Закупки", Table: "reg_purchases", Kind: accumulation.Turnovers,
})
