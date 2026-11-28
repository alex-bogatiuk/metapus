package item_automotive_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/configurations/automotive/domain/catalogs/item_automotive"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/catalogs/item"
)

func TestNormalizeArticleDropsSeparatorsAndCase(t *testing.T) {
	t.Parallel()
	for article, want := range map[string]string{
		"OC-90":           "OC90",
		"oc 90":           "OC90",
		" 0 986/452.041 ": "0986452041",
		"w712/75":         "W71275",
		"ab\tc":           "ABC",
		"":                "",
	} {
		require.Equal(t, want, item_automotive.NormalizeArticle(article), article)
	}
}

func TestBeforeWriteKeepsTheNormalizedArticle(t *testing.T) {
	t.Parallel()
	ext := &item_automotive.ItemAutomotive{NormalizedArticle: "STALE", Brand: " MANN "}
	require.NoError(t, item_automotive.BeforeWrite(context.Background(), &item.Item{Article: "W 712/75"}, ext))
	require.Equal(t, "W71275", ext.NormalizedArticle)
	require.NoError(t, ext.Validate(context.Background()))
	require.Equal(t, "MANN", ext.Brand)
}
