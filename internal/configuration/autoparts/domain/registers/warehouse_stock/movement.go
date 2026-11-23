// Package warehouse_stock is the accumulation register of physical stock on
// hand (ЗапасыНаСкладах): quantities by warehouse and item, without amounts.
package warehouse_stock

import (
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Movement is one movement of the register: a receipt adds the quantity to
// the warehouse's stock of the item, an expense takes it away.
type Movement struct {
	entity.MovementBase

	// Dimensions
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId"`
	ItemID      id.ID `db:"item_id" json:"itemId"`

	// Resources
	Quantity types.Quantity `db:"quantity" json:"quantity"`
}

// NewMovement creates a movement of the register.
func NewMovement(recorderID id.ID, recorderType string, recorderVersion int, period time.Time,
	recordType entity.RecordType, warehouseID, itemID id.ID, quantity types.Quantity) Movement {
	return Movement{
		MovementBase: entity.NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		WarehouseID:  warehouseID,
		ItemID:       itemID,
		Quantity:     quantity,
	}
}

// Balance is the stock of an item at a warehouse.
type Balance struct {
	WarehouseID id.ID          `db:"warehouse_id" json:"warehouseId"`
	ItemID      id.ID          `db:"item_id" json:"itemId"`
	Quantity    types.Quantity `db:"quantity" json:"quantity"`
}
