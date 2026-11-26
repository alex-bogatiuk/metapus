package goods_receipt

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

// ForCustomerOrder fills a goods receipt of what the customer order still
// needs bought: for each item what the order awaits less what is reserved
// for it, each line under the order so it arrives reserved. The supplier and
// purchase prices are the user's to fill.
func ForCustomerOrder(ctx context.Context, order *customer_order.CustomerOrder, receipt *GoodsReceipt) error {
	receipt.WarehouseID, receipt.CurrencyID = order.WarehouseID, order.CurrencyID

	awaited, err := customer_orders.Register.Balances(ctx, accumulation.Filter{"customer_order_id": order.ID})
	if err != nil {
		return err
	}
	need := map[id.ID]types.Quantity{}
	for _, b := range awaited {
		need[b.ItemID] += b.Quantity
	}
	reserved, err := stock.Register.Balances(ctx, accumulation.Filter{"warehouse_id": order.WarehouseID, "customer_order_id": order.ID})
	if err != nil {
		return err
	}
	for _, b := range reserved {
		need[b.ItemID] -= b.Quantity
	}

	for _, line := range order.Goods {
		quantity := min(line.Quantity, need[line.ItemID])
		if !quantity.IsPositive() {
			continue
		}
		need[line.ItemID] -= quantity
		receipt.Goods = append(receipt.Goods, GoodsLine{
			LineID: id.New(), LineNo: len(receipt.Goods) + 1, ItemID: line.ItemID, Quantity: quantity, CustomerOrderID: &order.ID,
		})
	}
	if len(receipt.Goods) == 0 {
		return apperror.NewValidation("everything the customer order awaits is reserved")
	}
	return nil
}
