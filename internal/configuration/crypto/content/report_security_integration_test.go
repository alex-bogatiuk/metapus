//go:build integration

package content_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/content"
	"metapus/pkg/sdk/appctx"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"

	"metapus/pkg/sdk/reports/compiler"
)

// The configuration's report datasets run against its freshly migrated schema.
func TestBuiltInJournalUsesFreshCryptoSchema(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(appctx.WithUser(context.Background(), &appctx.UserContext{UserID: id.New().String(), IsAdmin: true}), sdktest.NewTxManager(pool))
	for _, dataset := range content.AllDatasets() {
		_, err := reportCompiler(t).Execute(ctx, compiler.QueryRequest{Dataset: dataset.Key, Limit: 1})
		require.NoError(t, err, dataset.Key)
	}
}
