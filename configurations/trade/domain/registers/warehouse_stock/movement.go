// Package warehouse_stock is the accumulation register of physical stock on
// hand (ЗапасыНаСкладах): quantities by warehouse and item, without amounts.
// The register is declared: the platform stores its movements and balances,
// posts it and shows its movements from this description.
package warehouse_stock

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Movement is one movement of the register: a receipt adds the quantity to
// the warehouse's stock of the item, an expense takes it away.
type Movement struct {
	entity.MovementBase

	WarehouseID id.ID          `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" register:"dimension"`
	ItemID      id.ID          `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" register:"dimension"`
	Quantity    types.Quantity `db:"quantity" json:"quantity" meta:"label:Количество" register:"resource"`
}

// Register is the warehouse stock register (reg_warehouse_stock_movements,
// reg_warehouse_stock_balances). Stock never goes below zero: a document
// that would issue more than the warehouse holds is not posted.
var Register = accumulation.New[Movement](accumulation.Definition{
	Key: "warehouse_stock", Label: "Запасы на складах", Table: "reg_warehouse_stock", Kind: accumulation.Balances,
	NonNegative: true,
})
