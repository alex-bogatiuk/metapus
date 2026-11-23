// Package migration migrates tenant databases: the platform's, a
// configuration's and the extensions' migrations, each with a goose history
// of its own.
package migration

import (
	"io/fs"

	dbassets "github.com/alex-bogatiuk/metapus/db"
	"github.com/alex-bogatiuk/metapus/internal/core/version"
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
)

type (
	// Option configures Run.
	Option = core.Option
)

const (
	// PlatformVersionTable is the goose history of the platform's migrations.
	PlatformVersionTable = core.PlatformVersionTable
	// PlatformSchemaVersion is the platform's schema version this binary
	// expects every tenant database to be at: its highest tenant migration.
	PlatformSchemaVersion = version.ExpectedSchemaVersion
)

// CompatiblePlatformSchema reports whether a tenant database at platform
// schema version v is at PlatformSchemaVersion.
func CompatiblePlatformSchema(v int) bool { return version.CompatibleSchema(v) }

// HighestVersion returns the highest version of source's migrations: the
// schema version a database has once they are all applied. A nil source has
// version 0.
func HighestVersion(source fs.FS) (int, error) { return dbassets.HighestVersion(source) }

// ConfigurationVersionTable is the goose history of configuration name's
// migrations in a tenant database.
func ConfigurationVersionTable(name string) string { return core.ConfigurationVersionTable(name) }

// WithConfiguration makes Run apply configuration name's tenant migrations
// after the platform's; source is nil when the configuration ships none. A
// database that holds another configuration's migrations is refused. Without
// it Run applies the platform's and the extensions' migrations to a database
// of any configuration.
func WithConfiguration(name string, source fs.FS) core.Option {
	return core.WithConfiguration(name, source)
}
