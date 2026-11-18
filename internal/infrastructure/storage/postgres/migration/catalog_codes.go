package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/entity"
	"metapus/internal/core/numerator"
)

// WithCatalogCodes makes RunAll give every item of the given catalogs that
// is stored without a code one from its series (see BackfillCatalogCodes).
// Pass the registry's catalog Definitions from the composition root.
func WithCatalogCodes(definitions []entity.Definition) Option {
	return func(o *options) { o.catalogs = append(o.catalogs, definitions...) }
}

// CatalogCodeBackfill is the number of codes BackfillCatalogCodes assigned,
// by catalog key.
type CatalogCodeBackfill map[string]int

// BackfillCatalogCodes assigns codes to items stored with an empty code
// before the platform numbered catalogs, for each catalog that takes its
// codes from a series. It numbers them in id order from the same counter as
// CatalogService and skips codes already held, so later writes continue the
// series. Items marked for deletion keep their code, and catalogs whose code
// is derived or entered stay as they are. A catalog without its table in this
// database is skipped. Running it again assigns nothing.
func BackfillCatalogCodes(ctx context.Context, db *sql.DB, definitions []entity.Definition) (CatalogCodeBackfill, error) {
	result := CatalogCodeBackfill{}
	seen := map[string]bool{}
	for _, def := range definitions {
		if def.Kind != entity.CatalogKind || def.CodeSource() != entity.CodeFromSeries || seen[def.Key] {
			continue
		}
		seen[def.Key] = true
		assigned, err := backfillCatalog(ctx, db, def)
		if err != nil {
			return result, fmt.Errorf("catalog %s codes: %w", def.Key, err)
		}
		if assigned > 0 {
			result[def.Key] = assigned
		}
	}
	return result, nil
}

func backfillCatalog(ctx context.Context, db *sql.DB, def entity.Definition) (assigned int, err error) {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, def.Table).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	table := pgx.Identifier{def.Table}.Sanitize()
	series := numerator.CatalogSeries(def.CodeSeries())
	key := series.SequenceKey(time.Time{})

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM `+table+` WHERE code = '' AND deletion_mark = FALSE ORDER BY id FOR UPDATE`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, itemID)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	for _, itemID := range ids {
		code, err := nextFreeCode(ctx, tx, table, key, series)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET code = $1, version = version + 1 WHERE id = $2`, code, itemID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// nextFreeCode takes numbers of the series until one gives a code no item
// holds, as CatalogService does.
func nextFreeCode(ctx context.Context, tx *sql.Tx, table, key string, series numerator.Config) (string, error) {
	for range 100 {
		var n int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO sys_sequences (key, current_val) VALUES ($1, 1)
			ON CONFLICT (key) DO UPDATE SET current_val = sys_sequences.current_val + 1, updated_at = NOW()
			RETURNING current_val`, key).Scan(&n); err != nil {
			return "", err
		}
		code := series.Format(time.Time{}, n)
		var taken bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE code = $1 AND deletion_mark = FALSE)`, code).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return code, nil
		}
	}
	return "", fmt.Errorf("series %s: 100 consecutive codes are already taken", key)
}
