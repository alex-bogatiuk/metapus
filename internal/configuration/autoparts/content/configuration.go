// Package content assembles the auto parts store configuration
// («Автозапчасти»): its catalogs, documents and registers, its migrations
// (db/configuration/autoparts) and its part of the server. It is a skeleton
// that checks the platform carries a second industry configuration: the item
// and warehouse catalogs, the goods receipt and the warehouse stock register.
package content

import (
	"context"
	"fmt"
	"io/fs"

	dbassets "metapus/db"
	"metapus/internal/configuration"
	"metapus/internal/domain/integrations"
	"metapus/internal/domain/posting"
	"metapus/internal/infrastructure/http/contracts/spec"
	v1 "metapus/internal/infrastructure/http/v1"
)

// Name is the auto parts configuration's name.
const Name = "autoparts"

// Autoparts returns the auto parts store configuration.
func Autoparts() configuration.Configuration { return autoparts{} }

type autoparts struct{}

func (autoparts) Name() string { return Name }

func (autoparts) Register(reg *v1.FactoryRegistry) { register(reg) }

func (autoparts) RouteContracts() []spec.Bundle { return nil }

func (autoparts) Migrations() fs.FS {
	migrations, err := dbassets.ConfigurationMigrations(Name)
	if err != nil || migrations == nil {
		panic(fmt.Sprintf("autoparts migrations: %v", err)) // embedded at build time
	}
	return migrations
}

func (autoparts) IntegrationCatalog() *integrations.Catalog { return nil }

func (autoparts) PoolConsumers() []integrations.PoolConsumer { return nil }

// Server adds the warehouse stock register to posting.
func (autoparts) Server(context.Context, configuration.ServerEnv) (configuration.Server, error) {
	return configuration.Server{PostingModules: []posting.Module{NewRegisters()}}, nil
}

func (autoparts) Worker(context.Context, configuration.WorkerEnv) (configuration.Worker, error) {
	return configuration.Worker{}, nil
}
