package configuration

import (
	"fmt"

	dbassets "github.com/alex-bogatiuk/metapus/db"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/core/version"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/information"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
)

// MigrationOptions migrate a tenant database of the assembled application:
// the platform's migrations, the configuration's in a goose history of their
// own (migration.WithConfiguration), the extensions', then the permissions
// and catalog codes of the assembled entities and the check of their
// declarative registers' tables.
func MigrationOptions(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []migration.Option {
	return []migration.Option{
		migration.WithConfiguration(cfg.Name(), cfg.Migrations()),
		migration.WithPermissions(Permissions(cfg, extensions...)),
		migration.WithCatalogCodes(CatalogDefinitions(cfg, extensions...)),
		migration.WithAccumulationRegisters(AccumulationSchemas(cfg, extensions...)),
		migration.WithInformationRegisters(InformationSchemas(cfg, extensions...)),
	}
}

// AccumulationSchemas are the declarative accumulation registers of the
// assembled application, which the tenant migration checks against their
// tables.
func AccumulationSchemas(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []*accumulation.Schema {
	registers := Compose(cfg, extensions...).AccumulationRegisters()
	schemas := make([]*accumulation.Schema, len(registers))
	for i, r := range registers {
		schemas[i] = r.Schema()
	}
	return schemas
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

// InformationSchemas are the schemas of the declarative information
// registers cfg and the extensions register, which the tenant migration
// checks against the database.
func InformationSchemas(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []*information.Schema {
	registers := Compose(cfg, extensions...).InformationRegisters()
	schemas := make([]*information.Schema, len(registers))
	for i, r := range registers {
		schemas[i] = r.Schema()
	}
	return schemas
}
