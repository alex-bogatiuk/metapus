// Package stock is the accumulation register of the store's stock with its
// reserves (Запасы): quantities by warehouse, item and customer order. The
// free stock has no order (id.Nil); a reserve names the order it is held
// for. Reserving moves the quantity from the free stock to the order, so
// another customer cannot take it, and the order's own sale takes it back.
package stock

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Movement is one movement of the register.
type Movement struct {
	entity.MovementBase

	WarehouseID     id.ID          `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" register:"dimension"`
	ItemID          id.ID          `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" register:"dimension"`
	CustomerOrderID id.ID          `db:"customer_order_id" json:"customerOrderId" meta:"label:Заказ покупателя,ref:customer_order" register:"dimension"`
	Quantity        types.Quantity `db:"quantity" json:"quantity" meta:"label:Количество" register:"resource"`
}

// Register is the stock register (reg_stock_movements, reg_stock_balances).
// Neither the free stock nor a reserve goes below zero.
var Register = accumulation.New[Movement](accumulation.Definition{
	Key: "stock", Label: "Запасы", Table: "reg_stock", Kind: accumulation.Balances,
	NonNegative: true,
})

// Free is the order of the free stock: none.
func Free() id.ID { return id.Nil() }

// OrderOf is the order a reference names, the free stock without one.
func OrderOf(order *id.ID) id.ID {
	if order == nil {
		return Free()
	}
	return *order
}
