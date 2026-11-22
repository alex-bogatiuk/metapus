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

	"metapus/pkg/sdk/configuration"
	"metapus/pkg/sdk/contracts"
	"metapus/pkg/sdk/integrations"
	"metapus/pkg/sdk/posting"
	"metapus/pkg/sdk/registry"
)

// Name is the auto parts configuration's name.
const Name = "autoparts"

// Autoparts returns the auto parts store configuration.
func Autoparts() configuration.Configuration { return autoparts{} }

type autoparts struct{}

func (autoparts) Name() string { return Name }

// APIVersion: the configuration is built for the platform API 3.0.
func (autoparts) APIVersion() string { return "3.0.0" }

func (autoparts) Register(reg *registry.Registry) { register(reg) }

func (autoparts) RouteContracts() []contracts.Bundle { return nil }

func (autoparts) Migrations() fs.FS {
	migrations, err := configuration.EmbeddedMigrations(Name)
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
