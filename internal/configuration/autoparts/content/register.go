package content

import (
	"metapus/internal/configuration/autoparts/domain/catalogs/item"
	"metapus/internal/configuration/autoparts/domain/catalogs/warehouse"
	"metapus/internal/configuration/autoparts/domain/documents/goods_receipt"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

func register(reg *v1.FactoryRegistry) {
	reg.RegisterCatalog(v1.Catalog[item.Item](metadata.Presentation{
		Singular: "Номенклатура", Plural: "Номенклатура", NewLabel: "Новая позиция", Genitive: "позицию номенклатуры",
	}))
	reg.RegisterCatalog(v1.Catalog[warehouse.Warehouse](metadata.Presentation{
		Singular: "Склад", Plural: "Склады", NewLabel: "Новый склад", Genitive: "склад",
	}))
	reg.RegisterDocument(goodsReceiptRegistration{v1.Document[goods_receipt.GoodsReceipt](metadata.Presentation{
		Singular: "Приходная накладная", Plural: "Приходные накладные", NewLabel: "Новая приходная накладная", Genitive: "приходную накладную",
	})})
}

// goodsReceiptRegistration declares the fields behind the goods receipt's
// stock movements, so a field policy on the document hides them alike.
type goodsReceiptRegistration struct {
	*v1.AutoDocument[goods_receipt.GoodsReceipt, *goods_receipt.GoodsReceipt]
}

func (goodsReceiptRegistration) MovementSources() map[string][]string {
	return map[string][]string{
		"warehouse": {"warehouse_id"}, "item": {"goods.item_id"}, "quantity": {"goods.quantity"},
	}
}

var _ platform.MovementSourceProvider = goodsReceiptRegistration{}
