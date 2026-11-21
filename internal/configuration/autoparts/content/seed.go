package content

import (
	"context"
	"fmt"

	"metapus/internal/configuration"
	"metapus/internal/core/id"
)

// Seed adds the demo warehouses and a few parts with env.Demo; without it
// the configuration seeds nothing.
func (autoparts) Seed(ctx context.Context, env configuration.SeedEnv) error {
	if !env.Demo {
		return nil
	}
	for _, w := range []struct{ code, name, address string }{
		{"WH-000001", "Торговый зал", "г. Москва, ул. Ленина, 1"},
		{"WH-000002", "Основной склад", "г. Москва, ул. Складская, 5"},
		{"WH-000003", "Склад брака", "г. Москва, ул. Складская, 5"},
	} {
		if _, err := env.Pool.Exec(ctx, `
			INSERT INTO cat_warehouses (id, code, name, address)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (code) WHERE _deleted_at IS NULL AND code != '' DO NOTHING
		`, id.New(), w.code, w.name, w.address); err != nil {
			return fmt.Errorf("seed warehouse %s: %w", w.code, err)
		}
	}
	for _, i := range []struct{ code, name, article, manufacturer, unit string }{
		{"IT-000001", "Фильтр масляный", "W 712/75", "MANN-FILTER", "шт."},
		{"IT-000002", "Колодки тормозные передние", "0 986 494 524", "Bosch", "комплект"},
		{"IT-000003", "Масло моторное 5W-30, 4 л", "152648", "Castrol", "шт."},
	} {
		if _, err := env.Pool.Exec(ctx, `
			INSERT INTO cat_items (id, code, name, article, manufacturer, unit)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (code) WHERE _deleted_at IS NULL AND code != '' DO NOTHING
		`, id.New(), i.code, i.name, i.article, i.manufacturer, i.unit); err != nil {
			return fmt.Errorf("seed item %s: %w", i.code, err)
		}
	}
	env.Logger.Info("autoparts demo data seeded: 3 warehouses, 3 items")
	return nil
}
