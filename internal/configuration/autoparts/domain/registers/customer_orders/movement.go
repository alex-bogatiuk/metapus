// Package customer_orders is the accumulation register of what customers
// ordered and were not yet given (ЗаказыПокупателей): quantities by order and
// item. The order adds its lines, a goods issue under the order takes what
// it ships; no order is shipped more than it holds.
package customer_orders

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Movement is one movement of the register.
type Movement struct {
	entity.MovementBase

	CustomerOrderID id.ID          `db:"customer_order_id" json:"customerOrderId" meta:"label:Заказ покупателя,ref:customer_order" register:"dimension"`
	ItemID          id.ID          `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" register:"dimension"`
	Quantity        types.Quantity `db:"quantity" json:"quantity" meta:"label:Количество" register:"resource"`
}

// Register is the customer orders register (reg_customer_orders_movements,
// reg_customer_orders_balances).
var Register = accumulation.New[Movement](accumulation.Definition{
	Key: "customer_orders", Label: "Заказы покупателей", Table: "reg_customer_orders", Kind: accumulation.Balances,
	NonNegative: true,
})
