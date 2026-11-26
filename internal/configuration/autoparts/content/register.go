package content

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/catalogs/item"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/catalogs/warehouse"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/customer_order"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/goods_issue"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/goods_receipt"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/customer_orders"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/stock"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/warehouse_stock"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registry"
)

func register(reg *registry.Registry) {
	reg.RegisterCatalog(registry.Catalog[item.Item](registry.Presentation{
		Singular: "Номенклатура", Plural: "Номенклатура", NewLabel: "Новая позиция", Genitive: "позицию номенклатуры",
	}))
	reg.RegisterCatalog(registry.Catalog[warehouse.Warehouse](registry.Presentation{
		Singular: "Склад", Plural: "Склады", NewLabel: "Новый склад", Genitive: "склад",
	}))
	reg.RegisterAccumulation(warehouse_stock.Register)
	reg.RegisterAccumulation(stock.Register)
	reg.RegisterAccumulation(customer_orders.Register)
	reg.RegisterDocument(customerOrderRegistration{registry.Document[customer_order.CustomerOrder](registry.Presentation{
		Singular: "Заказ покупателя", Plural: "Заказы покупателей", NewLabel: "Новый заказ покупателя", Genitive: "заказ покупателя",
	})})
	reg.RegisterDocument(goodsReceiptRegistration{registry.Document[goods_receipt.GoodsReceipt](registry.Presentation{
		Singular: "Приходная накладная", Plural: "Приходные накладные", NewLabel: "Новая приходная накладная", Genitive: "приходную накладную",
	})})
	reg.RegisterDocument(goodsIssueRegistration{registry.Document[goods_issue.GoodsIssue](registry.Presentation{
		Singular: "Расходная накладная", Plural: "Расходные накладные", NewLabel: "Новая расходная накладная", Genitive: "расходную накладную",
	})})
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
		"warehouse_id": {"warehouse_id"}, "item_id": {"goods.item_id"}, "quantity": {"goods.quantity", "goods.reserve"},
	}
}

var _ registry.MovementSourceProvider = customerOrderRegistration{}
