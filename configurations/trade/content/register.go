package content

import (
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/catalogs/item"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/catalogs/order_state"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/catalogs/warehouse"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/documents/customer_order"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/documents/goods_issue"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/documents/goods_receipt"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/documents/month_closing"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/documents/price_setting"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/customer_orders"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/inventory_cost"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/item_prices"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/order_state_history"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/purchases"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/sales"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/stock"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/registers/warehouse_stock"
	"github.com/alex-bogatiuk/metapus/configurations/trade/infrastructure/storage/postgres/order_state_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/metadata"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registry"
)

func register(reg *registry.Registry) {
	reg.RegisterCatalog(registry.Catalog[item.Item](registry.Presentation{
		Singular: "Номенклатура", Plural: "Номенклатура", NewLabel: "Новая позиция", Genitive: "позицию номенклатуры",
	}))
	reg.RegisterCatalog(registry.Catalog[warehouse.Warehouse](registry.Presentation{
		Singular: "Склад", Plural: "Склады", NewLabel: "Новый склад", Genitive: "склад",
	}))
	statuses := make([]metadata.EnumValue, len(order_state.Values))
	for i, v := range order_state.Values {
		statuses[i] = metadata.EnumValue{Value: v.Value, Label: v.Label}
	}
	metadata.RegisterEnum[order_state.Status](statuses)
	reg.RegisterCatalog(registry.Catalog[order_state.State](registry.Presentation{
		Singular: "Состояние заказа", Plural: "Состояния заказов покупателей", NewLabel: "Новое состояние", Genitive: "состояние заказа",
	}))
	reg.RegisterAccumulation(warehouse_stock.Register)
	reg.RegisterAccumulation(stock.Register)
	reg.RegisterAccumulation(customer_orders.Register)
	reg.RegisterAccumulation(purchases.Register)
	reg.RegisterAccumulation(sales.Register)
	reg.RegisterAccumulation(inventory_cost.Register)
	reg.RegisterInformation(order_state_history.Register)
	reg.RegisterInformation(item_prices.Register)
	reg.RegisterDocument(customerOrderRegistration{registry.Document[customer_order.CustomerOrder](registry.Presentation{
		Singular: "Заказ покупателя", Plural: "Заказы покупателей", NewLabel: "Новый заказ покупателя", Genitive: "заказ покупателя",
	}).Configure(func(s *domain.BaseHeaderDocumentService[*customer_order.CustomerOrder]) {
		// The state decides the order's movements: its status, and what a
		// closed order cancels and releases, are set before every save.
		hook := customer_order.BeforeSave(order_state_repo.New())
		s.GetHooks().OnBeforeCreate(hook)
		s.GetHooks().OnBeforeUpdate(hook)
	})})
	reg.RegisterDocument(goodsReceiptRegistration{registry.Document[goods_receipt.GoodsReceipt](registry.Presentation{
		Singular: "Приходная накладная", Plural: "Приходные накладные", NewLabel: "Новая приходная накладная", Genitive: "приходную накладную",
	})})
	reg.RegisterDocument(goodsIssueRegistration{registry.Document[goods_issue.GoodsIssue](registry.Presentation{
		Singular: "Расходная накладная", Plural: "Расходные накладные", NewLabel: "Новая расходная накладная", Genitive: "расходную накладную",
	})})
	reg.RegisterDocument(registry.Document[price_setting.PriceSetting](registry.Presentation{
		Singular: "Установка цен", Plural: "Установки цен номенклатуры", NewLabel: "Новая установка цен", Genitive: "установку цен",
	}))
	reg.RegisterDocument(registry.Document[month_closing.MonthClosing](registry.Presentation{
		Singular: "Закрытие месяца", Plural: "Закрытия месяца", NewLabel: "Новое закрытие месяца", Genitive: "закрытие месяца",
	}))
	reg.RegisterBasis(registry.Basis("Расходная накладная", goods_issue.FromCustomerOrder))
	reg.RegisterBasis(registry.Basis("Приходная накладная под заказ", goods_receipt.ForCustomerOrder))
}

// goodsReceiptRegistration declares the fields behind the goods receipt's
// stock movements, so a field policy on the document hides them alike.
type goodsReceiptRegistration struct {
	*registry.AutoDocument[goods_receipt.GoodsReceipt, *goods_receipt.GoodsReceipt]
}

func (goodsReceiptRegistration) MovementSources() map[string][]string {
	return map[string][]string{
		"warehouse_id": {"warehouse_id"}, "item_id": {"goods.item_id"}, "quantity": {"goods.quantity"},
		"customer_order_id": {"goods.customer_order_id"},
	}
}

var _ registry.MovementSourceProvider = goodsReceiptRegistration{}

// goodsIssueRegistration declares the fields behind the goods issue's stock
// movements, as goodsReceiptRegistration does for the receipt.
type goodsIssueRegistration struct {
	*registry.AutoDocument[goods_issue.GoodsIssue, *goods_issue.GoodsIssue]
}

func (goodsIssueRegistration) MovementSources() map[string][]string {
	return map[string][]string{
		"warehouse_id": {"warehouse_id"}, "item_id": {"goods.item_id"}, "quantity": {"goods.quantity", "goods.reserve"},
		"customer_order_id": {"customer_order_id"},
	}
}

var _ registry.MovementSourceProvider = goodsIssueRegistration{}

// customerOrderRegistration declares the fields behind the customer order's
// movements; the order is its own customer_order_id.
type customerOrderRegistration struct {
	*registry.AutoDocument[customer_order.CustomerOrder, *customer_order.CustomerOrder]
}

func (customerOrderRegistration) MovementSources() map[string][]string {
	return map[string][]string{
		"warehouse_id": {"warehouse_id"}, "item_id": {"goods.item_id"},
		"quantity": {"goods.quantity", "goods.reserve", "goods.cancelled", "goods.released"},
	}
}

var _ registry.MovementSourceProvider = customerOrderRegistration{}
