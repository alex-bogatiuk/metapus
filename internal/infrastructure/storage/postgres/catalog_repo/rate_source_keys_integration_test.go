//go:build integration

package catalog_repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
)

func TestRateSourceKeysMoveToPools(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := tenant.Context(appctx.UserContext{})
	source := func(code, sourceType string, key *string, marked bool) id.ID {
		uid := id.New()
		_, err := tenant.Pool.Exec(ctx, `INSERT INTO cat_rate_sources(id,code,name,source_type,api_key,deletion_mark) VALUES($1,$2,$2,$3,$4,$5)`, uid, code, sourceType, key, marked)
		require.NoError(t, err)
		return uid
	}
	pool := func(code, provider string, marked bool) id.ID {
		uid := id.New()
		_, err := tenant.Pool.Exec(ctx, `INSERT INTO cat_integration_pools(id,code,name,provider,deletion_mark) VALUES($1,$2,$2,$3,$4)`, uid, code, provider, marked)
		require.NoError(t, err)
		return uid
	}
	key := func(value string) *string { return &value }
	second := source("CG-B", "coingecko", key("key-b"), false)
	first := source("CG-A", "coingecko", key("key-a"), false)
	source("CG-NOKEY", "coingecko", nil, false)
	source("CG-MARKED", "coingecko", key("key-marked"), true)
	binance := source("BN-A", "binance", key("key-bn"), false)

	keys, err := catalog_repo.RateSourcesWithOwnKey(ctx, "coingecko")
	require.NoError(t, err)
	require.Equal(t, []catalog_repo.RateSourceKey{{ID: first, Code: "CG-A", APIKey: "key-a"}, {ID: second, Code: "CG-B", APIKey: "key-b"}}, keys)

	demo := pool("POOL-CG", "coingecko_demo", false)
	require.NoError(t, tenant.Tx.RunInTransaction(ctx, func(ctx context.Context) error {
		return catalog_repo.MoveRateSourceKeyToPool(ctx, first, demo)
	}))
	var poolID *id.ID
	var stored *string
	var version int
	require.NoError(t, tenant.Pool.QueryRow(ctx, `SELECT integration_pool_id,api_key,version FROM cat_rate_sources WHERE id=$1`, first).Scan(&poolID, &stored, &version))
	require.NotNil(t, poolID)
	require.Equal(t, demo, *poolID)
	require.Nil(t, stored)
	require.Equal(t, 2, version)
	keys, err = catalog_repo.RateSourcesWithOwnKey(ctx, "coingecko")
	require.NoError(t, err)
	require.Len(t, keys, 1)

	for _, wrong := range []id.ID{pool("POOL-OTHER", "getblock_net", false), pool("POOL-GONE", "coingecko_demo", true), id.New()} {
		err = tenant.Tx.RunInTransaction(ctx, func(ctx context.Context) error {
			return catalog_repo.MoveRateSourceKeyToPool(ctx, second, wrong)
		})
		var appErr *apperror.AppError
		require.True(t, errors.As(err, &appErr), "%v", err)
		require.Equal(t, apperror.CodeValidation, appErr.Code)
	}
	for _, wrong := range []id.ID{binance, id.New()} {
		err = tenant.Tx.RunInTransaction(ctx, func(ctx context.Context) error {
			return catalog_repo.MoveRateSourceKeyToPool(ctx, wrong, demo)
		})
		require.True(t, apperror.IsNotFound(err), "%v", err)
	}
	require.NoError(t, tenant.Pool.QueryRow(ctx, `SELECT api_key FROM cat_rate_sources WHERE id=$1`, second).Scan(&stored))
	require.Equal(t, "key-b", *stored, "a refused move keeps the source's own key")
}
