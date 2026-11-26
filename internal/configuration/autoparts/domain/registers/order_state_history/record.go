// Package order_state_history is the information register of customer
// order states over time (ИсторияСостоянийЗаказов): from which moment an
// order was in which state, and who set it. The order writes a record each
// time its state changes.
package order_state_history

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/catalogs/order_state"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/information"
)

// Record is the state an order took at its period.
type Record struct {
	information.RecordBase

	CustomerOrderID id.ID              `db:"customer_order_id" json:"customerOrderId" meta:"label:Заказ покупателя,ref:customer_order" register:"dimension"`
	StateID         id.ID              `db:"state_id" json:"stateId" meta:"label:Состояние,ref:customer_order_state" register:"resource"`
	Status          order_state.Status `db:"status" json:"status" meta:"label:Статус" register:"resource"`
	AuthorID        id.ID              `db:"author_id" json:"authorId" meta:"label:Автор,ref:user" register:"resource"`
}

// Register is the order state history (reg_order_state_history).
var Register = information.New[Record](information.Definition{
	Key: "order_state_history", Label: "История состояний заказов", Table: "reg_order_state_history",
	Periodicity: information.Second, Mode: information.Independent,
})
