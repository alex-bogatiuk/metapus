// Package content assembles the base configuration «Торговля и сервис»
// (trade): the business logic a trading company automates its work with on
// its own — items and warehouses, customer orders with reserves, goods
// receipts and issues, item prices, the cost of goods sold and the month
// closing — its migrations (db/configuration/trade) and its part of the
// server. Add-ons (configuration.AddOn) build industry specifics on it,
// such as the auto parts shop and service «Автобизнес»
// (configurations/automotive).
//
// The packages of configurations/trade/domain are the base's public API for
// its add-ons: Version grows its minor version with additions to them and
// its major version with changes that break an add-on.
package content

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/contracts"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registry"
)

// Name is the trade configuration's name.
const Name = "trade"

// Version is the version of the configuration add-ons are built for.
const Version = "1.0.0"

// Trade returns the base configuration «Торговля и сервис».
func Trade() configuration.Configuration { return trade{} }

type trade struct{}

func (trade) Name() string { return Name }

// Version: see the package documentation.
func (trade) Version() string { return Version }

// APIVersion: the configuration is built for the platform API 3.4.
func (trade) APIVersion() string { return "3.4.0" }

func (trade) Register(reg *registry.Registry) { register(reg) }

func (trade) RouteContracts() []contracts.Bundle { return nil }

func (trade) Migrations() fs.FS {
	migrations, err := configuration.EmbeddedMigrations(Name)
	if err != nil || migrations == nil {
		panic(fmt.Sprintf("trade migrations: %v", err)) // embedded at build time
	}
	return migrations
}

func (trade) IntegrationCatalog() *integrations.Catalog { return nil }

func (trade) PoolConsumers() []integrations.PoolConsumer { return nil }

// Server adds nothing: the configuration's registers are declared
// (RegisterAccumulation, RegisterInformation) and the platform posts them.
func (trade) Server(context.Context, configuration.ServerEnv) (configuration.Server, error) {
	return configuration.Server{}, nil
}

func (trade) Worker(context.Context, configuration.WorkerEnv) (configuration.Worker, error) {
	return configuration.Worker{}, nil
}

var _ configuration.Versioned = trade{}
