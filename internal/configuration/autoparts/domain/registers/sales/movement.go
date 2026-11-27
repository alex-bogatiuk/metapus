// Package sales is the turnover register of sales (Продажи): quantities,
// revenue and cost of goods sold, by warehouse, item, customer and
// currency. The goods issue writes the quantity and the revenue in its
// currency; the month closing writes the cost of each month's sales in the
// accounting currency, so the gross profit is the revenue less the cost.
package sales

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Movement is one sale, or the cost of a month's sales.
type Movement struct {
	entity.MovementBase

	WarehouseID id.ID            `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" register:"dimension"`
	ItemID      id.ID            `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" register:"dimension"`
	CustomerID  id.ID            `db:"customer_id" json:"customerId" meta:"label:Покупатель,ref:counterparty" register:"dimension"`
	CurrencyID  id.ID            `db:"currency_id" json:"currencyId" meta:"label:Валюта,ref:currency" register:"dimension"`
	Quantity    types.Quantity   `db:"quantity" json:"quantity" meta:"label:Количество" register:"resource"`
	Revenue     types.MinorUnits `db:"revenue" json:"revenue" meta:"label:Выручка" register:"resource,currency=currency_id"`
	Cost        types.MinorUnits `db:"cost" json:"cost" meta:"label:Себестоимость" register:"resource,currency=currency_id"`
}

// Register is the sales register (reg_sales_movements).
var Register = accumulation.New[Movement](accumulation.Definition{
	Key: "sales", Label: "Продажи", Table: "reg_sales", Kind: accumulation.Turnovers,
})
