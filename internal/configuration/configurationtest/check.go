// Package configurationtest checks that a configuration keeps the
// configuration.Configuration contract. Every configuration runs Check from
// its own tests.
package configurationtest

import (
	"io/fs"
	"path"
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	dbassets "metapus/db"
	"metapus/internal/configuration"
	"metapus/internal/infrastructure/http/contracts"
	"metapus/internal/infrastructure/http/contracts/spec"
)

var migrationName = regexp.MustCompile(`^\d{5}_[a-z0-9_]+\.sql$`)

// Check verifies cfg: its name, that it registers the same entities each
// time, that the assembled application's permissions and routes are unique,
// that its wire contracts generate, and that its migrations are the ones the
// repository holds for it, in the platform's format and versions of their own.
func Check(t *testing.T, cfg configuration.Configuration) {
	t.Helper()
	require.Truef(t, configuration.ValidName(cfg.Name()), "configuration name %q must be a lowercase directory name", cfg.Name())

	t.Run("registration", func(t *testing.T) {
		first := permissionCodes(cfg)
		require.Equal(t, first, permissionCodes(cfg), "Register must add the same entities to every registry")
		require.Equal(t, len(first), len(slices.Compact(slices.Clone(first))), "duplicate permission codes: %v", first)
	})

	t.Run("contracts", func(t *testing.T) {
		bundles := append([]spec.Bundle{configuration.Compose(cfg).HTTPContracts()}, cfg.RouteContracts()...)
		seen := map[string]bool{}
		for _, route := range contracts.Routes(bundles...) {
			key := route.Scope + " " + route.Method + " " + route.Path
			require.Falsef(t, seen[key], "route %s is declared twice", key)
			seen[key] = true
		}
		_, err := contracts.Generate(contracts.Registry(bundles...))
		require.NoError(t, err)
	})

	t.Run("migrations", func(t *testing.T) {
		checkMigrations(t, cfg)
	})
}

func permissionCodes(cfg configuration.Configuration) []string {
	permissions := configuration.Permissions(cfg)
	codes := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		codes = append(codes, permission.Code)
	}
	slices.Sort(codes)
	return codes
}

func checkMigrations(t *testing.T, cfg configuration.Configuration) {
	t.Helper()
	shipped := sqlFiles(t, dbassets.Files, path.Join("configuration", cfg.Name()))
	migrations := cfg.Migrations()
	if migrations == nil {
		require.Emptyf(t, shipped, "db/configuration/%s holds migrations, but the configuration returns none", cfg.Name())
		return
	}
	own := sqlFiles(t, migrations, ".")
	require.Equalf(t, shipped, own, "the configuration's migrations must be db/configuration/%s", cfg.Name())

	platform := map[string]bool{}
	for _, name := range sqlFiles(t, dbassets.Files, "migrations") {
		platform[name[:5]] = true
	}
	for _, name := range own {
		require.Truef(t, migrationName.MatchString(name), "%s: migration file name must be NNNNN_name.sql", name)
		require.Falsef(t, platform[name[:5]], "%s reuses a platform migration version", name)
		raw, err := fs.ReadFile(migrations, name)
		require.NoError(t, err)
		require.NoErrorf(t, dbassets.CheckMigrationFormat(string(raw)), "%s", name)
	}
}

// sqlFiles lists the .sql file names in dir of source; a missing dir has none.
func sqlFiles(t *testing.T, source fs.FS, dir string) []string {
	t.Helper()
	entries, err := fs.ReadDir(source, dir)
	if err != nil {
		require.ErrorIs(t, err, fs.ErrNotExist)
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && path.Ext(entry.Name()) == ".sql" {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return names
}
