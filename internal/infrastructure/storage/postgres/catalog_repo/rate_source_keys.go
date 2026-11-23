package catalog_repo

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// RateSourceKey is a rate source that still keeps its provider key on the
// source itself instead of an integration pool.
type RateSourceKey struct {
	ID     id.ID
	Code   string
	APIKey string
}

// RateSourcesWithOwnKey lists, in code order, the rate sources of the type
// that are not marked for deletion and keep a key of their own.
func RateSourcesWithOwnKey(ctx context.Context, sourceType string) ([]RateSourceKey, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT id, code, api_key FROM cat_rate_sources
		WHERE source_type = $1 AND api_key IS NOT NULL AND api_key <> ''
		  AND NOT deletion_mark AND _deleted_at IS NULL
		ORDER BY code`, sourceType)
	if err != nil {
		return nil, fmt.Errorf("list rate source keys: %w", err)
	}
	defer rows.Close()
	out := []RateSourceKey{}
	for rows.Next() {
		var key RateSourceKey
		if err := rows.Scan(&key.ID, &key.Code, &key.APIKey); err != nil {
			return nil, fmt.Errorf("scan rate source key: %w", err)
		}
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list rate source keys: %w", err)
	}
	return out, nil
}

// MoveRateSourceKeyToPool makes the rate source take its key from the pool
// and drops the key it kept itself, under the rules of the catalog form:
// only a CoinGecko source takes a pool, and only a CoinGecko Demo pool that
// is not marked for deletion.
func MoveRateSourceKeyToPool(ctx context.Context, sourceID, poolID id.ID) error {
	if err := LockIntegrationConfiguration(ctx); err != nil {
		return err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var valid bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_integration_pools WHERE id=$1 AND provider='coingecko_demo' AND NOT deletion_mark AND _deleted_at IS NULL)`, poolID).Scan(&valid); err != nil {
		return fmt.Errorf("check rate source pool: %w", err)
	}
	if !valid {
		return apperror.NewValidation("Выберите пул CoinGecko Demo")
	}
	tag, err := q.Exec(ctx, `
		UPDATE cat_rate_sources SET integration_pool_id=$2, api_key=NULL, version=version+1
		WHERE id=$1 AND source_type='coingecko' AND NOT deletion_mark AND _deleted_at IS NULL`, sourceID, poolID)
	if err != nil {
		return fmt.Errorf("move rate source key to pool: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewNotFound("rate_source", sourceID.String())
	}
	return nil
}
