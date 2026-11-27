//go:build integration

package content_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/configurations/trade/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/migration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
)

// The base runs alone: its tenant has no add-on history.
func TestTradeRunsWithoutAnAddOn(t *testing.T) {
	db := sdktest.NewTenantWithMigrations(t, migration.WithConfiguration(content.Name, content.Trade().Migrations()))
	var addOn bool
	require.NoError(t, db.Pool.QueryRow(db.AdminContext(), `SELECT to_regclass($1) IS NOT NULL`, migration.ConfigurationVersionTable("automotive")).Scan(&addOn))
	require.False(t, addOn)
	_, versioned := content.Trade().(configuration.Versioned)
	require.True(t, versioned, "add-ons are built for a version of the base")
}
