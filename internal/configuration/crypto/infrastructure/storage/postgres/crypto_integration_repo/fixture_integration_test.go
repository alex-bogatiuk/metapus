//go:build integration

package crypto_integration_repo

import (
	"context"
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/cryptoschema"
	"metapus/pkg/sdk/integrations"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/id"
)

// fixtureDB creates an isolated, migrated tenant database for one test.
func fixtureDB(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_INTEGRATIONS_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_INTEGRATIONS_DSN must identify an isolated test cluster")
	}
	root, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(root.Close)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	name := "integrations_" + id.Compact(id.New())
	_, err = root.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := root.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		if err != nil {
			t.Errorf("clean up integration fixture database: %v", err)
		}
	})
	u.Path = "/" + name
	output, err := sdktest.Migrate(u.String(), cryptoschema.Migration())
	require.NoError(t, err, output)
	p, err := pgxpool.New(context.Background(), u.String())
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return tenant.WithTxManager(context.Background(), sdktest.NewTxManager(p)), p
}

// appDefinition returns a provider's definition from the application catalog.
func appDefinition(provider integrations.Provider) integrations.ProviderDefinition {
	definition, err := cryptointegrations.Catalog().Require(provider)
	if err != nil {
		panic(err)
	}
	return definition
}
