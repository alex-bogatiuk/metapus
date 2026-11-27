// Package order_state is the catalog of customer order states (Состояния
// заказов покупателей): the store's own names for where an order is
// («Ожидает предоплаты», «Готов к выдаче»), each of one status that decides
// what the order does in the registers.
package order_state

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "customer_order_state", Name: "CustomerOrderState", Route: "customer-order-states", Table: "cat_customer_order_states",
	Kind: entity.CatalogKind,
	// An empty code is numbered from the catalog's series (OS-000001).
	Code: entity.CodeRule{Prefix: "OS"},
}

// Status is what an order of the state does (статус заказа).
type Status string

const (
	// Open (Открыт): the order is taken but not worked on; it reserves
	// nothing and awaits no shipment.
	Open Status = "open"
	// InProgress (В работе): the order reserves its lines' reserve and
	// awaits shipment of its quantities.
	InProgress Status = "in_progress"
	// Completed (Выполнен) and Cancelled (Отменён) close the order: what was
	// not shipped is cancelled and its reserve goes back to the free stock.
	Completed Status = "completed"
	Cancelled Status = "cancelled"
)

// Values are the statuses with their names, in the order the UI lists them.
var Values = []entity.StatusValue{
	{Value: string(Open), Label: "Открыт"},
	{Value: string(InProgress), Label: "В работе"},
	{Value: string(Completed), Label: "Выполнен"},
	{Value: string(Cancelled), Label: "Отменён"},
}

// Closed reports whether an order of the status is closed.
func (s Status) Closed() bool { return s == Completed || s == Cancelled }

// Moves reports whether an order of the status moves the registers.
func (s Status) Moves() bool { return s == InProgress || s.Closed() }

func (s Status) valid() bool { return s == Open || s == InProgress || s.Closed() }

// State is one customer order state.
type State struct {
	entity.Catalog
	Status Status `db:"status" json:"status" meta:"label:Статус" binding:"required"`
}

func (State) EntityDefinition() entity.Definition { return Definition }

func (e *State) Validate(ctx context.Context) error {
	if err := e.Catalog.Validate(ctx); err != nil {
		return err
	}
	if !e.Status.valid() {
		return apperror.NewValidation(fmt.Sprintf("status %q is not known", e.Status))
	}
	return nil
}

// The states every tenant starts with (db/configuration/trade/00001).
var (
	New             = id.MustParse("01960000-0000-7000-8000-000000000001") // Новый
	AwaitingPayment = id.MustParse("01960000-0000-7000-8000-000000000002") // Ожидает предоплаты
	Working         = id.MustParse("01960000-0000-7000-8000-000000000003") // В работе
	ReadyForPickup  = id.MustParse("01960000-0000-7000-8000-000000000004") // Готов к выдаче
	Done            = id.MustParse("01960000-0000-7000-8000-000000000005") // Выполнен
	Rejected        = id.MustParse("01960000-0000-7000-8000-000000000006") // Отменён
)

// Statuses reads the status of a state; the configuration's storage
// implements it (infrastructure/storage/postgres/order_state_repo).
type Statuses interface {
	StatusOf(ctx context.Context, state id.ID) (Status, error)
}
