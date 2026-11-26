package goods_issue

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/customer_order"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/customer_orders"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/stock"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// FromCustomerOrder fills a goods issue that ships the customer order: the
// order's customer, warehouse and currency, and for each item what the
// order still awaits, from its reserve first. An order with nothing left to
// ship (or not posted) gives no issue.
func FromCustomerOrder(ctx context.Context, order *customer_order.CustomerOrder, issue *GoodsIssue) error {
	issue.CustomerID, issue.WarehouseID, issue.CurrencyID = order.CustomerID, order.WarehouseID, order.CurrencyID
	issue.CustomerOrderID = &order.ID

	awaited, err := customer_orders.Register.Balances(ctx, accumulation.Filter{"customer_order_id": order.ID})
	if err != nil {
		return err
	}
	left := map[id.ID]types.Quantity{}
	for _, b := range awaited {
		left[b.ItemID] += b.Quantity
	}
	reserved, err := stock.Register.Balances(ctx, accumulation.Filter{"warehouse_id": order.WarehouseID, "customer_order_id": order.ID})
	if err != nil {
		return err
	}
	reserve := map[id.ID]types.Quantity{}
	for _, b := range reserved {
		reserve[b.ItemID] += b.Quantity
	}

	for _, line := range order.Goods {
		quantity := min(line.Quantity, left[line.ItemID])
		if !quantity.IsPositive() {
			continue
		}
		left[line.ItemID] -= quantity
		fromReserve := max(min(quantity, reserve[line.ItemID]), 0)
		reserve[line.ItemID] -= fromReserve
		issue.Goods = append(issue.Goods, GoodsLine{
			LineID: id.New(), LineNo: len(issue.Goods) + 1, ItemID: line.ItemID,
			Quantity: quantity, Reserve: fromReserve, Price: line.Price, Amount: share(line.Amount, quantity, line.Quantity),
		})
	}
	if len(issue.Goods) == 0 {
		return apperror.NewValidation("the customer order has nothing left to ship")
	}
	return nil
}

// share is the part of a line's amount for part of its quantity.
func share(amount types.MinorUnits, part, whole types.Quantity) types.MinorUnits {
	if part == whole || whole == 0 {
		return amount
	}
	return types.MinorUnits(int64(amount) * int64(part) / int64(whole))
}
