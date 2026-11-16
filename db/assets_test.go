package dbassets

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestTenantMigrationsJoinPlatformAndConfigurationsIntoOneSequence(t *testing.T) {
	source := fstest.MapFS{
		"migrations/00001_core.sql":      {Data: []byte("core")},
		"migrations/00003_late_core.sql": {Data: []byte("late")},
		"configuration/b/00002_b.sql":    {Data: []byte("b")},
		"configuration/a/00004_a.sql":    {Data: []byte("a")},
		"configuration/a/README.md":      {Data: []byte("ignored")},
		"meta/00001_tenants.sql":         {Data: []byte("meta is separate")},
	}
	flat, err := tenantMigrations(source)
	require.NoError(t, err)
	entries, err := fs.ReadDir(flat, ".")
	require.NoError(t, err)
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	require.Equal(t, []string{"00001_core.sql", "00002_b.sql", "00003_late_core.sql", "00004_a.sql"}, names)
	raw, err := fs.ReadFile(flat, "00002_b.sql")
	require.NoError(t, err)
	require.Equal(t, "b", string(raw))
	matches, err := fs.Glob(flat, "*.sql")
	require.NoError(t, err)
	require.Len(t, matches, 4)
}

func TestTenantMigrationsRejectReusedVersionsAndBadNames(t *testing.T) {
	_, err := tenantMigrations(fstest.MapFS{
		"migrations/00001_core.sql":   {Data: []byte("core")},
		"configuration/x/00001_x.sql": {Data: []byte("x")},
	})
	require.ErrorContains(t, err, "version 00001")
	_, err = tenantMigrations(fstest.MapFS{"configuration/x/7_x.sql": {Data: []byte("x")}, "migrations/00001_core.sql": {}})
	require.ErrorContains(t, err, "NNNNN_name.sql")
}

func TestEmbeddedTenantMigrationsAreConsistent(t *testing.T) {
	flat, err := TenantMigrations()
	require.NoError(t, err)
	matches, err := fs.Glob(flat, "*.sql")
	require.NoError(t, err)
	require.NotEmpty(t, matches)
}
