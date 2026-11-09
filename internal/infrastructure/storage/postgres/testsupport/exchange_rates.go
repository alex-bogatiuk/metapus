//go:build integration

// Package testsupport provisions isolated fixtures; it never resets the supplied database.
package testsupport

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"net/url"
	"os"
	"testing"
)

// ExchangeRateDSN creates a fresh database in the explicitly configured test cluster.
// Databases remain available for inspecting failed tests until the test container is removed.
func ExchangeRateDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_EXCHANGE_RATES_DSN")
	require.NotEmpty(t, dsn, "an isolated PostgreSQL test cluster is required")
	target, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, target.Scheme)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	name := "exchange_rates_" + id.Compact(id.New())
	_, err = pool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	target.Path = "/" + name
	return target.String()
}
