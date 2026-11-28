package content

import (
	"github.com/alex-bogatiuk/metapus/configurations/automotive/domain/catalogs/item_automotive"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/catalogs/item"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registry"
)

// Group is the form group of the add-on's fields of the base's objects.
const Group = "Автобизнес"

// ItemExtension is the add-on's extension of the base's items: the brand and
// the normalized article, kept by the write hook and searched by the list.
var ItemExtension = registry.Extend[item.Item, item_automotive.ItemAutomotive](Name, Group).
	OnWrite(item_automotive.BeforeWrite).
	Search("normalized_article", item_automotive.NormalizeArticle)

func register(reg *registry.Registry) {
	reg.RegisterExtension(ItemExtension)
}
