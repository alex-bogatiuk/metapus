//go:build integration

package content_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/content"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/infrastructure/storage/postgres"
)

// The configuration's report datasets run against its freshly migrated schema.
func TestBuiltInJournalUsesFreshCryptoSchema(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(appctx.WithUser(context.Background(), &appctx.UserContext{UserID: id.New().String(), IsAdmin: true}), postgres.NewTxManagerFromRawPool(pool))
	for _, dataset := range content.AllDatasets() {
		_, err := reportCompiler(t).Execute(ctx, compiler.QueryRequest{Dataset: dataset.Key, Limit: 1})
		require.NoError(t, err, dataset.Key)
	}
}
