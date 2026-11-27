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
// the platform's migrations, the base configuration's and the add-on's, each
// in a goose history of their own (migration.WithConfiguration,
// WithAddOn), the extensions', then the permissions
// and catalog codes of the assembled entities and the check of their
// declarative registers' tables.
func MigrationOptions(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []migration.Option {
	stack := Stack(cfg)
	options := []migration.Option{migration.WithConfiguration(stack[0].Name(), stack[0].Migrations())}
	if len(stack) == 2 {
		options = append(options, migration.WithAddOn(stack[1].Name(), stack[1].Migrations()))
	}
	return append(options,
		migration.WithPermissions(Permissions(cfg, extensions...)),
		migration.WithCatalogCodes(CatalogDefinitions(cfg, extensions...)),
		migration.WithAccumulationRegisters(AccumulationSchemas(cfg, extensions...)),
		migration.WithInformationRegisters(InformationSchemas(cfg, extensions...)),
	)
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
// base configuration's migrations and of the add-on's.
func ExpectedSchema(cfg Configuration) (tenant.Schema, error) {
	stack := Stack(cfg)
	configurationVersion, err := dbassets.HighestVersion(stack[0].Migrations())
	if err != nil {
		return tenant.Schema{}, fmt.Errorf("configuration %s: %w", stack[0].Name(), err)
	}
	schema := tenant.Schema{Configuration: stack[0].Name(), Version: version.ExpectedSchemaVersion, ConfigurationVersion: configurationVersion}
	if len(stack) == 2 {
		addOnVersion, err := dbassets.HighestVersion(stack[1].Migrations())
		if err != nil {
			return tenant.Schema{}, fmt.Errorf("add-on %s: %w", stack[1].Name(), err)
		}
		schema.AddOn, schema.AddOnVersion = stack[1].Name(), addOnVersion
	}
	return schema, nil
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
