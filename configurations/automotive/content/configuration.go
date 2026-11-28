// Package content assembles the add-on «Автобизнес» (automotive): the
// specifics of an auto parts shop and an auto service (СТО) on the base
// configuration «Торговля и сервис» (configurations/trade). The add-on works
// with that base only: it imports the base's packages and the platform's
// public API (pkg/sdk), and the platform runs the base first and the add-on
// on top of it.
//
// The shop and the service are one add-on, since a tenant runs at most one:
// a parts shop that opens a service bay switches the service on with a
// functional option instead of changing its add-on (docs/automotive).
//
// The add-on adds its fields to the base's objects (registry.Extend): the
// brand and the normalized article of an item, in its own tables one to one
// with the base's (db/configuration/automotive).
package content

import (
	"context"
	"fmt"
	"io/fs"

	trade "github.com/alex-bogatiuk/metapus/configurations/trade/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/contracts"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registry"
)

// Name is the add-on's name.
const Name = "automotive"

// Automotive returns the add-on «Автобизнес».
func Automotive() configuration.AddOn { return automotive{} }

type automotive struct{}

func (automotive) Name() string { return Name }

// APIVersion: the add-on is built for the platform API 3.10 (extensions of
// the base's catalogs).
func (automotive) APIVersion() string { return "3.10.0" }

func (automotive) Base() configuration.Configuration { return trade.Trade() }

// BaseVersion: the add-on is built for «Торговля и сервис» 1.0.
func (automotive) BaseVersion() string { return "1.0.0" }

func (automotive) Register(reg *registry.Registry) { register(reg) }

func (automotive) RouteContracts() []contracts.Bundle { return nil }

// Migrations are the add-on's tables (db/configuration/automotive), in a
// goose history of their own.
func (automotive) Migrations() fs.FS {
	migrations, err := configuration.EmbeddedMigrations(Name)
	if err != nil || migrations == nil {
		panic(fmt.Sprintf("automotive migrations: %v", err)) // embedded at build time
	}
	return migrations
}

func (automotive) IntegrationCatalog() *integrations.Catalog { return nil }

func (automotive) PoolConsumers() []integrations.PoolConsumer { return nil }

func (automotive) Server(context.Context, configuration.ServerEnv) (configuration.Server, error) {
	return configuration.Server{}, nil
}

func (automotive) Worker(context.Context, configuration.WorkerEnv) (configuration.Worker, error) {
	return configuration.Worker{}, nil
}
