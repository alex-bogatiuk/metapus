// Package item_prices is the information register of item selling prices
// (ЦеныНоменклатуры): the price of an item from a day on, set by the price
// setting document. An order takes a line's price from it when the line has
// none.
package item_prices

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/information"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Record is an item's price from its period on.
type Record struct {
	information.RecordBase

	ItemID     id.ID            `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" register:"dimension"`
	Price      types.MinorUnits `db:"price" json:"price" meta:"label:Цена" register:"resource,currency=currency_id"`
	CurrencyID id.ID            `db:"currency_id" json:"currencyId" meta:"label:Валюта,ref:currency" register:"resource"`
}

// Register is the item prices register (reg_item_prices).
var Register = information.New[Record](information.Definition{
	Key: "item_prices", Label: "Цены номенклатуры", Table: "reg_item_prices",
	Periodicity: information.Day, Mode: information.Recorder,
})
