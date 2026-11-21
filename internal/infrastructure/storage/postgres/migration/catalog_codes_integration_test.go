//go:build integration

package migration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// Items stored without a code before the platform numbered catalogs get one
// from the catalog's series on migration; deleted items, derived catalogs and
// catalogs without a table stay as they are.
func TestRunAllNumbersCatalogItemsStoredWithoutACode(t *testing.T) {
	db := migratedPermissionDB(t)
	ctx := context.Background()
	key := "backfill_probe_" + strings.ToLower(id.Compact(id.New()))[:12]
	table := "cat_" + key
	_, err := db.Exec(`CREATE TABLE ` + table + ` (id UUID PRIMARY KEY, code VARCHAR(20) NOT NULL, deletion_mark BOOLEAN NOT NULL DEFAULT FALSE, version INT NOT NULL DEFAULT 1)`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE ` + table) })
	const (
		first   = "00000000-0000-7000-8000-000000000001"
		manual  = "00000000-0000-7000-8000-000000000002"
		second  = "00000000-0000-7000-8000-000000000003"
		deleted = "00000000-0000-7000-8000-000000000004"
	)
	_, err = db.Exec(`INSERT INTO `+table+` (id, code, deletion_mark) VALUES ($1, '', FALSE), ($2, 'BP-000002', FALSE), ($3, '', FALSE), ($4, '', TRUE)`,
		first, manual, second, deleted)
	require.NoError(t, err)

	probe := entity.Definition{Kind: entity.CatalogKind, Key: key, Name: "BackfillProbe", Route: "backfill-probes", Table: table, Code: entity.CodeRule{Prefix: "BP"}}
	derived := entity.Definition{Kind: entity.CatalogKind, Key: key + "_derived", Name: "DerivedProbe", Route: "derived-probes", Table: table, Code: entity.CodeRule{Source: entity.CodeDerived}}
	missing := entity.Definition{Kind: entity.CatalogKind, Key: key + "_missing", Name: "MissingProbe", Route: "missing-probes", Table: table + "_missing"}
	output, err := RunAll(os.Getenv("METAPUS_TEST_MIGRATION_DSN"), withCrypto(t), WithCatalogCodes([]entity.Definition{probe, derived, missing}))
	require.NoError(t, err, output)
	require.Contains(t, output, "[catalog codes] "+key+": 2 assigned")

	codes := map[string]string{}
	versions := map[string]int{}
	rows, err := db.Query(`SELECT id, code, version FROM ` + table)
	require.NoError(t, err)
	for rows.Next() {
		var itemID, code string
		var version int
		require.NoError(t, rows.Scan(&itemID, &code, &version))
		codes[itemID], versions[itemID] = code, version
	}
	require.NoError(t, rows.Close())
	require.Equal(t, map[string]string{first: "BP-000001", manual: "BP-000002", second: "BP-000003", deleted: ""}, codes,
		"items are numbered in id order and skip a code taken by hand")
	require.Equal(t, 2, versions[first], "a numbered item gets a new version, so a stale edit cannot clear it")
	require.Equal(t, 1, versions[manual])

	var counter int64
	require.NoError(t, db.QueryRow(`SELECT current_val FROM sys_sequences WHERE key = $1`, "catalog."+key).Scan(&counter))
	require.Equal(t, int64(3), counter, "later writes continue the series after the backfill")

	again, err := BackfillCatalogCodes(ctx, db, []entity.Definition{probe})
	require.NoError(t, err)
	require.Empty(t, again, "a second run assigns nothing")
}
