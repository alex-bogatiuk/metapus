// Package cryptoschema is the crypto configuration's part of a tenant
// database schema: its migrations, which have a goose history of their own,
// and the check that a tenant database is at their version.
package cryptoschema

import (
	"fmt"
	"io/fs"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/migration"
)

// Name is the crypto-processing configuration's name.
const Name = "crypto"

// Migrations returns the crypto configuration's tenant migrations
// (db/configuration/crypto).
func Migrations() fs.FS {
	migrations, err := configuration.EmbeddedMigrations(Name)
	if err != nil || migrations == nil {
		panic(fmt.Sprintf("crypto migrations: %v", err)) // embedded at build time
	}
	return migrations
}

// ExpectedVersion is the highest version of the crypto migrations: the crypto
// schema version this binary expects every tenant database to be at.
var ExpectedVersion = func() int {
	v, err := migration.HighestVersion(Migrations())
	if err != nil {
		panic(fmt.Sprintf("crypto schema version: %v", err))
	}
	return v
}()

// VersionQuery selects a tenant database's platform and crypto schema
// versions, the highest migration applied from each history.
var VersionQuery = `SELECT
	(SELECT COALESCE(MAX(version_id), 0) FROM ` + migration.PlatformVersionTable + ` WHERE is_applied),
	(SELECT COALESCE(MAX(version_id), 0) FROM ` + migration.ConfigurationVersionTable(Name) + ` WHERE is_applied)`

// Compatible reports whether a tenant database at the platform and crypto
// schema versions is at the versions this binary expects.
func Compatible(platform, crypto int) bool {
	return migration.CompatiblePlatformSchema(platform) && crypto == ExpectedVersion
}

// Mismatch describes a tenant database at other versions than expected.
func Mismatch(platform, crypto int) error {
	return fmt.Errorf("tenant database schema is platform %d, crypto %d; expected platform %d, crypto %d",
		platform, crypto, migration.PlatformSchemaVersion, ExpectedVersion)
}

// Migration makes migration.Run apply the crypto migrations after the
// platform's.
func Migration() migration.Option { return migration.WithConfiguration(Name, Migrations()) }
