package customer_order

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/catalogs/order_state"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/customer_orders"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/stock"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// StateHook is ApplyState with the store of states, for the order service's
// before-create and before-update hooks.
func StateHook(statuses order_state.Statuses) func(context.Context, *CustomerOrder) error {
	return func(ctx context.Context, order *CustomerOrder) error { return ApplyState(ctx, statuses, order) }
}

// ApplyState runs before the order is saved, in its transaction: it gives a
// new order the state «Новый», takes the status of the order's state and,
// for a closed order, works out from the registers how much of each line is
// cancelled and how much reserve is released, so that after the order's
// posting it awaits nothing more and holds no reserve. RegisterMovements
// then stays a function of the document. An order that is not closed
// cancels and releases nothing, so reopening one restores it.
func ApplyState(ctx context.Context, statuses order_state.Statuses, order *CustomerOrder) error {
	if order.StateID == nil {
		state := order_state.New
		order.StateID = &state
	}
	status, err := statuses.StatusOf(ctx, *order.StateID)
	if err != nil {
		return err
	}
	order.Status = status
	for i := range order.Goods {
		order.Goods[i].Cancelled, order.Goods[i].Released = 0, 0
	}
	if !status.Closed() {
		return nil
	}

	// What the order's current posting adds, per item: awaited, and the
	// order's own reserve less what it released.
	ownAwaited, ownReserve, err := order.ownMovements(ctx)
	if err != nil {
		return err
	}
	awaited, err := customer_orders.Register.Balances(ctx, accumulation.Filter{"customer_order_id": order.ID})
	if err != nil {
		return err
	}
	reserved, err := stock.Register.Balances(ctx, accumulation.Filter{"warehouse_id": order.WarehouseID, "customer_order_id": order.ID})
	if err != nil {
		return err
	}
	// Left over once the order is reposted with nothing cancelled or
	// released: the balance now, less the current posting, plus the new one.
	cancel := map[id.ID]types.Quantity{}
	release := map[id.ID]types.Quantity{}
	for _, b := range awaited {
		cancel[b.ItemID] += b.Quantity
	}
	for _, b := range reserved {
		release[b.ItemID] += b.Quantity
	}
	for item, q := range ownAwaited {
		cancel[item] -= q
	}
	for item, q := range ownReserve {
		release[item] -= q
	}
	for _, line := range order.Goods {
		cancel[line.ItemID] += line.Quantity
		release[line.ItemID] += line.Reserve
	}

	for i := range order.Goods {
		line := &order.Goods[i]
		line.Cancelled = max(min(cancel[line.ItemID], line.Quantity), 0)
		cancel[line.ItemID] -= line.Cancelled
		line.Released = max(release[line.ItemID], 0)
		release[line.ItemID] = 0
	}
	return nil
}

// ownMovements sums, per item, what the order's posted movements add to
// what it awaits and to its reserve.
func (e *CustomerOrder) ownMovements(ctx context.Context) (awaited, reserve map[id.ID]types.Quantity, err error) {
	awaited, reserve = map[id.ID]types.Quantity{}, map[id.ID]types.Quantity{}
	orders, err := customer_orders.Register.Movements(ctx, e.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range orders {
		awaited[m.ItemID] += signed(m.RecordType, m.Quantity)
	}
	moves, err := stock.Register.Movements(ctx, e.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range moves {
		if m.CustomerOrderID == e.ID {
			reserve[m.ItemID] += signed(m.RecordType, m.Quantity)
		}
	}
	return awaited, reserve, nil
}

func signed(kind entity.RecordType, q types.Quantity) types.Quantity {
	if kind == entity.RecordTypeExpense {
		return -q
	}
	return q
}
