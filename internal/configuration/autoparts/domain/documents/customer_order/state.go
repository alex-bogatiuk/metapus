package customer_order

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/catalogs/order_state"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/customer_orders"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/item_prices"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/order_state_history"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/stock"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/appctx"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/information"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// BeforeSave is the order service's before-create and before-update hook:
// lines without a price take the item's price (FillPrices), the state sets
// the status and what a closed order gives up (ApplyState), and a changed
// state is written to the order state history (RecordState).
func BeforeSave(statuses order_state.Statuses) func(context.Context, *CustomerOrder) error {
	return func(ctx context.Context, order *CustomerOrder) error {
		if err := FillPrices(ctx, order); err != nil {
			return err
		}
		if err := ApplyState(ctx, statuses, order); err != nil {
			return err
		}
		return RecordState(ctx, order, time.Now())
	}
}

// RecordState writes the order's state to the history at the moment at,
// unless the history already has it as the order's latest state.
func RecordState(ctx context.Context, order *CustomerOrder, at time.Time) error {
	if order.StateID == nil {
		return nil
	}
	last, err := order_state_history.Register.SliceLast(ctx, at, information.Filter{"customer_order_id": order.ID})
	if err != nil {
		return err
	}
	if len(last) > 0 && last[0].StateID == *order.StateID {
		return nil
	}
	author, err := id.Parse(appctx.GetUserID(ctx))
	if err != nil {
		author = id.Nil()
	}
	return order_state_history.Register.Set(ctx, order_state_history.Record{
		RecordBase:      information.RecordBase{Period: at},
		CustomerOrderID: order.ID, StateID: *order.StateID, Status: order.Status, AuthorID: author,
	})
}

// FillPrices gives each line without a price the item's price on the
// order's date, if it is set in the order's currency, and the line's amount
// at that price.
func FillPrices(ctx context.Context, order *CustomerOrder) error {
	var items []id.ID
	for _, line := range order.Goods {
		if line.Price.IsZero() {
			items = append(items, line.ItemID)
		}
	}
	if len(items) == 0 {
		return nil
	}
	prices, err := item_prices.Register.SliceLast(ctx, order.Date, information.Filter{"item_id": items})
	if err != nil {
		return err
	}
	byItem := map[id.ID]types.MinorUnits{}
	for _, p := range prices {
		if p.CurrencyID == order.CurrencyID {
			byItem[p.ItemID] = p.Price
		}
	}
	for i := range order.Goods {
		line := &order.Goods[i]
		if price, ok := byItem[line.ItemID]; ok && line.Price.IsZero() {
			line.Price = price
			line.Amount = types.MinorUnits(int64(price) * int64(line.Quantity) / types.QuantityScale)
		}
	}
	return nil
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
