// Package item_automotive holds the «Автобизнес» fields of the base's items
// (configurations/trade/domain/catalogs/item): the brand of a part and its
// normalized article, by which the shop finds a part whatever way a
// supplier writes it (OC-90, OC 90, oc90).
//
// The brand is a string for now: the brand catalog (part_brand) with its
// synonyms and quality classes comes with references in extensions. The
// base's string Manufacturer stays for the items outside the auto business.
package item_automotive

import (
	"context"
	"strings"
	"unicode"

	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/catalogs/item"
)

// ItemAutomotive is the add-on's extension of an item, stored in
// cat_items__automotive one to one with cat_items.
type ItemAutomotive struct {
	// Brand is the brand of the part (MANN-FILTER, BOSCH), as the supplier's
	// price list names it.
	Brand string `db:"brand" json:"brand" meta:"label:Бренд"`
	// NormalizedArticle is the item's article normalized by
	// NormalizeArticle; the write hook keeps it, the list search uses it.
	NormalizedArticle string `db:"normalized_article" json:"normalizedArticle" meta:"label:Нормализованный артикул,readOnly:true"`
}

// Validate trims the brand.
func (e *ItemAutomotive) Validate(context.Context) error {
	e.Brand = strings.TrimSpace(e.Brand)
	return nil
}

// NormalizeArticle is an article as the shop searches it: upper case,
// without "-", "/", "." and spaces.
func NormalizeArticle(article string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || r == '/' || r == '.' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, article))
}

// BeforeWrite keeps the normalized article of the item before every write.
func BeforeWrite(_ context.Context, it *item.Item, ext *ItemAutomotive) error {
	ext.NormalizedArticle = NormalizeArticle(it.Article)
	return nil
}
