//go:build integration

package content_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/configurations/automotive/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/migration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
)

// A tenant of the add-on holds the platform's, the base's and the add-on's
// migrations, each in a goose history of its own, and the base's tables.
func TestAnAutomotiveTenantHoldsItsBase(t *testing.T) {
	db := sdktest.NewTenant(t, content.Automotive())
	ctx := db.AdminContext()
	for _, history := range []string{migration.PlatformVersionTable, migration.ConfigurationVersionTable("trade"), migration.ConfigurationVersionTable(content.Name)} {
		var exists bool
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, history).Scan(&exists))
		require.True(t, exists, history)
	}
	var tables bool
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT to_regclass('cat_items') IS NOT NULL AND to_regclass('doc_month_closings') IS NOT NULL`).Scan(&tables))
	require.True(t, tables, "the base's tables")
}
