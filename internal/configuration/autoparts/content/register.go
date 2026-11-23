package content

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/catalogs/item"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/catalogs/warehouse"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/goods_receipt"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registry"
)

func register(reg *registry.Registry) {
	reg.RegisterCatalog(registry.Catalog[item.Item](registry.Presentation{
		Singular: "Номенклатура", Plural: "Номенклатура", NewLabel: "Новая позиция", Genitive: "позицию номенклатуры",
	}))
	reg.RegisterCatalog(registry.Catalog[warehouse.Warehouse](registry.Presentation{
		Singular: "Склад", Plural: "Склады", NewLabel: "Новый склад", Genitive: "склад",
	}))
	reg.RegisterDocument(goodsReceiptRegistration{registry.Document[goods_receipt.GoodsReceipt](registry.Presentation{
		Singular: "Приходная накладная", Plural: "Приходные накладные", NewLabel: "Новая приходная накладная", Genitive: "приходную накладную",
	})})
}

// goodsReceiptRegistration declares the fields behind the goods receipt's
// stock movements, so a field policy on the document hides them alike.
type goodsReceiptRegistration struct {
	*registry.AutoDocument[goods_receipt.GoodsReceipt, *goods_receipt.GoodsReceipt]
}

func (goodsReceiptRegistration) MovementSources() map[string][]string {
	return map[string][]string{
		"warehouse": {"warehouse_id"}, "item": {"goods.item_id"}, "quantity": {"goods.quantity"},
	}
}

var _ registry.MovementSourceProvider = goodsReceiptRegistration{}
