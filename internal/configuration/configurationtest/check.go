// Package configurationtest checks that a configuration keeps the
// configuration.Configuration contract. Every configuration runs Check from
// its own tests.
package configurationtest

import (
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	dbassets "github.com/alex-bogatiuk/metapus/db"
	"github.com/alex-bogatiuk/metapus/internal/configuration"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts"
)

var migrationName = regexp.MustCompile(`^\d{5}_[a-z0-9_]+\.sql$`)

// Check verifies cfg: its name, that it registers the same entities each
// time, that the assembled application's permissions and routes are unique,
// that its wire contracts and frontend files generate, that its integration
// catalog keeps the platform's kinds and providers, and that its migrations
// are the ones the repository holds for it, in the platform's format and
// versions of their own.
func Check(t *testing.T, cfg configuration.Configuration) {
	t.Helper()
	require.Truef(t, configuration.ValidName(cfg.Name()), "configuration name %q must be a lowercase directory name", cfg.Name())

	t.Run("api version", func(t *testing.T) {
		require.NoError(t, configuration.CheckAPIVersion(cfg))
	})

	t.Run("registration", func(t *testing.T) {
		first := permissionCodes(cfg)
		require.Equal(t, first, permissionCodes(cfg), "Register must add the same entities to every registry")
		require.Equal(t, len(first), len(slices.Compact(slices.Clone(first))), "duplicate permission codes: %v", first)
	})

	t.Run("contracts", func(t *testing.T) {
		bundles := configuration.HTTPContractBundles(cfg)
		seen := map[string]bool{}
		for _, route := range contracts.Routes(bundles...) {
			key := route.Scope + " " + route.Method + " " + route.Path
			require.Falsef(t, seen[key], "route %s is declared twice", key)
			seen[key] = true
		}
		_, err := contracts.Generate(contracts.Registry(bundles...))
		require.NoError(t, err)
		_, _, err = configuration.HTTPArtifacts(cfg)
		require.NoError(t, err)
	})

	t.Run("integrations", func(t *testing.T) {
		catalog := configuration.IntegrationCatalog(cfg)
		for _, kind := range integrations.PlatformKinds() {
			require.Truef(t, slices.ContainsFunc(catalog.Kinds(), func(k integrations.KindDefinition) bool { return k.ID == kind.ID }),
				"the integration catalog misses platform kind %s", kind.ID)
		}
		for _, provider := range integrations.PlatformProviders() {
			_, ok := catalog.Definition(provider.ID)
			require.Truef(t, ok, "the integration catalog misses platform provider %s", provider.ID)
		}
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
	// A configuration in the platform's repository ships its migrations in
	// db/configuration/<name>/; one in its own repository embeds its own.
	if len(shipped) > 0 {
		require.Equalf(t, shipped, own, "the configuration's migrations must be db/configuration/%s", cfg.Name())
	}
	require.NotEmptyf(t, own, "the configuration returns migrations without a NNNNN_name.sql file")

	// The configuration's migrations have a goose history of their own; the
	// versions from dbassets.FirstExtensionMigration are the extension
	// modules'.
	for _, name := range own {
		require.Truef(t, migrationName.MatchString(name), "%s: migration file name must be NNNNN_name.sql", name)
		version, err := strconv.Atoi(name[:5])
		require.NoError(t, err)
		require.Lessf(t, version, dbassets.FirstExtensionMigration, "%s: a configuration's migration version is below %d", name, dbassets.FirstExtensionMigration)
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
