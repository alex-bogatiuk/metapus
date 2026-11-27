// Package inventory_cost is the accumulation register of the cost of the
// stock (Себестоимость запасов): quantities and their cost by warehouse,
// item and the accounting currency. Only the month closing writes it: the
// month's purchases come in, the month's sales go out at the month's
// average cost, so the balance at a month's start is the cost of the stock
// left from the closed months.
package inventory_cost

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Movement is the month's purchases (receipt) or sales (expense) of an
// item at a warehouse.
type Movement struct {
	entity.MovementBase

	WarehouseID id.ID            `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" register:"dimension"`
	ItemID      id.ID            `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" register:"dimension"`
	CurrencyID  id.ID            `db:"currency_id" json:"currencyId" meta:"label:Валюта учёта,ref:currency" register:"dimension"`
	Quantity    types.Quantity   `db:"quantity" json:"quantity" meta:"label:Количество" register:"resource"`
	Amount      types.MinorUnits `db:"amount" json:"amount" meta:"label:Себестоимость" register:"resource,currency=currency_id"`
}

// Register is the stock cost register (reg_inventory_cost_movements,
// reg_inventory_cost_balances).
var Register = accumulation.New[Movement](accumulation.Definition{
	Key: "inventory_cost", Label: "Себестоимость запасов", Table: "reg_inventory_cost", Kind: accumulation.Balances,
})

// Sequence is the sequence of documents the cost of the stock depends on
// (entity.SequenceRule): goods receipts and issues are in it, the month
// closing restores it.
const Sequence = "inventory_cost"
