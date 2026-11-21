package configuration

import (
	"fmt"

	dbassets "metapus/db"
	"metapus/internal/core/tenant"
	"metapus/internal/core/version"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/storage/postgres/migration"
)

// MigrationOptions migrate a tenant database of the assembled application:
// the platform's migrations, the configuration's in a goose history of their
// own (migration.WithConfiguration), the extensions', then the permissions
// and catalog codes of the assembled entities.
func MigrationOptions(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []migration.Option {
	return []migration.Option{
		migration.WithConfiguration(cfg.Name(), cfg.Migrations()),
		migration.WithPermissions(Permissions(cfg, extensions...)),
		migration.WithCatalogCodes(CatalogDefinitions(cfg, extensions...)),
	}
}

// ExpectedSchema is the schema of a tenant database migrated by this binary
// with cfg: the platform's schema version and the highest version of the
// configuration's migrations.
func ExpectedSchema(cfg Configuration) (tenant.Schema, error) {
	configurationVersion, err := dbassets.HighestVersion(cfg.Migrations())
	if err != nil {
		return tenant.Schema{}, fmt.Errorf("configuration %s: %w", cfg.Name(), err)
	}
	return tenant.Schema{Configuration: cfg.Name(), Version: version.ExpectedSchemaVersion, ConfigurationVersion: configurationVersion}, nil
}
