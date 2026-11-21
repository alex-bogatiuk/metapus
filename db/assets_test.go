package dbassets

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestCheckMigrationFormat(t *testing.T) {
	const up = "-- +goose Up\n"
	const down = "-- +goose Down\n"
	const begin = "-- +goose StatementBegin\n"
	const end = "-- +goose StatementEnd\n"
	const statement = "SELECT 1;\n"
	const block = begin + statement + end
	valid := "-- migration notes\n" + up + block + "\n-- rollback\n" + down + block
	require.NoError(t, CheckMigrationFormat(valid))
	t.Run("CRLF and no final newline", func(t *testing.T) {
		require.NoError(t, CheckMigrationFormat(strings.ReplaceAll(strings.TrimSuffix(valid, "\n"), "\n", "\r\n")))
	})
	t.Run("goose options", func(t *testing.T) {
		sql := "-- +goose NO TRANSACTION\n" + up + begin + "-- +goose ENVSUB ON\n" + statement + "-- +goose ENVSUB OFF\n" + end + down + block
		require.NoError(t, CheckMigrationFormat(sql))
	})

	for _, tt := range []struct{ name, sql, want string }{
		{"both ends in Down", up + begin + statement + down + block + end, "unexpected goose directive"},
		{"SQL before Up", statement + up + block + down + block, "outside"},
		{"SQL after Up block", up + block + statement + down + block, "outside"},
		{"SQL after Down block", up + block + down + block + statement, "outside"},
		{"duplicate block in Up", up + block + block + down + block, "unexpected goose directive"},
		{"comment before begin", up + "-- note\n" + block + down + block, "immediately"},
		{"missing Down end", up + block + down + begin + statement, "expected -- +goose StatementEnd"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, CheckMigrationFormat(tt.sql), tt.want)
		})
	}
}

func TestExtensionMigrationsJoinModulesIntoOneSequence(t *testing.T) {
	source := fstest.MapFS{
		"migrations/00001_core.sql":     {Data: []byte("platform is separate")},
		"extensions/b/10002_b.sql":      {Data: []byte("b")},
		"extensions/a/10001_a.sql":      {Data: []byte("a")},
		"extensions/a/10003_late_a.sql": {Data: []byte("late")},
		"extensions/README.md":          {Data: []byte("ignored")},
		"configuration/x/00006_x.sql":   {Data: []byte("configuration is separate")},
	}
	flat, err := extensionMigrations(source)
	require.NoError(t, err)
	entries, err := fs.ReadDir(flat, ".")
	require.NoError(t, err)
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	require.Equal(t, []string{"10001_a.sql", "10002_b.sql", "10003_late_a.sql"}, names)
	raw, err := fs.ReadFile(flat, "10002_b.sql")
	require.NoError(t, err)
	require.Equal(t, "b", string(raw))
}

func TestExtensionMigrationsRejectReusedVersionsLowVersionsAndBadNames(t *testing.T) {
	_, err := extensionMigrations(fstest.MapFS{
		"extensions/a/10001_a.sql": {Data: []byte("a")},
		"extensions/b/10001_b.sql": {Data: []byte("b")},
	})
	require.ErrorContains(t, err, "version 10001")
	_, err = extensionMigrations(fstest.MapFS{"extensions/a/00007_a.sql": {Data: []byte("a")}})
	require.ErrorContains(t, err, "starts at 10000")
	_, err = extensionMigrations(fstest.MapFS{"extensions/a/7_a.sql": {Data: []byte("a")}})
	require.ErrorContains(t, err, "NNNNN_name.sql")
}

func TestHighestVersionSkipsExtensionVersions(t *testing.T) {
	version, err := HighestVersion(fstest.MapFS{
		"00001_core.sql": {}, "00009_last.sql": {}, "10001_ext.sql": {}, "README.md": {},
	})
	require.NoError(t, err)
	require.Equal(t, 9, version)
	version, err = HighestVersion(nil)
	require.NoError(t, err)
	require.Zero(t, version)
	_, err = HighestVersion(fstest.MapFS{"1_bad.sql": {}})
	require.ErrorContains(t, err, "NNNNN_name.sql")
}

func TestEmbeddedMigrationsAreConsistent(t *testing.T) {
	platform, err := PlatformMigrations()
	require.NoError(t, err)
	matches, err := fs.Glob(platform, "*.sql")
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	_, err = ExtensionMigrations()
	require.NoError(t, err)
	configurations, err := fs.Glob(Files, "configuration/*")
	require.NoError(t, err)
	for _, dir := range configurations {
		source, err := ConfigurationMigrations(strings.TrimPrefix(dir, "configuration/"))
		require.NoError(t, err, dir)
		require.NotNil(t, source, dir)
	}
	missing, err := ConfigurationMigrations("no_such_configuration")
	require.NoError(t, err)
	require.Nil(t, missing)
}
