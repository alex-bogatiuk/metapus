package content

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/configurations/automotive/domain/catalogs/item_automotive"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

// Seed gives the base's demo items, seeded by SQL without the add-on's
// write hook, their automotive fields: the brand from the demo's
// manufacturer and the normalized article. Without env.Demo it seeds
// nothing; an item that already has its row keeps it.
func (automotive) Seed(ctx context.Context, env configuration.SeedEnv) error {
	if !env.Demo {
		return nil
	}
	rows, err := env.Pool.Query(ctx, `SELECT i.id, i.article, i.manufacturer FROM cat_items i
		WHERE NOT i.is_folder AND NOT EXISTS (SELECT 1 FROM cat_items__automotive a WHERE a.id = i.id)`)
	if err != nil {
		return fmt.Errorf("read demo items: %w", err)
	}
	type demoItem struct {
		id                    id.ID
		article, manufacturer string
	}
	var items []demoItem
	for rows.Next() {
		var item demoItem
		if err := rows.Scan(&item.id, &item.article, &item.manufacturer); err != nil {
			rows.Close()
			return fmt.Errorf("read demo items: %w", err)
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read demo items: %w", err)
	}
	for _, item := range items {
		if _, err := env.Pool.Exec(ctx, `INSERT INTO cat_items__automotive (id, brand, normalized_article) VALUES ($1, $2, $3)
			ON CONFLICT (id) DO NOTHING`, item.id, item.manufacturer, item_automotive.NormalizeArticle(item.article)); err != nil {
			return fmt.Errorf("seed the automotive fields of item %s: %w", item.id, err)
		}
	}
	env.Logger.Info(fmt.Sprintf("automotive demo data seeded: %d items", len(items)))
	return nil
}
