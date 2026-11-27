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
// The add-on adds nothing yet: a tenant of it is a trade tenant that will
// get the automotive objects and fields as they come.
package content

import (
	"context"
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

// APIVersion: the add-on is built for the platform API 3.8.
func (automotive) APIVersion() string { return "3.8.0" }

func (automotive) Base() configuration.Configuration { return trade.Trade() }

// BaseVersion: the add-on is built for «Торговля и сервис» 1.0.
func (automotive) BaseVersion() string { return "1.0.0" }

func (automotive) Register(*registry.Registry) {}

func (automotive) RouteContracts() []contracts.Bundle { return nil }

// Migrations: the add-on has no tables yet.
func (automotive) Migrations() fs.FS { return nil }

func (automotive) IntegrationCatalog() *integrations.Catalog { return nil }

func (automotive) PoolConsumers() []integrations.PoolConsumer { return nil }

func (automotive) Server(context.Context, configuration.ServerEnv) (configuration.Server, error) {
	return configuration.Server{}, nil
}

func (automotive) Worker(context.Context, configuration.WorkerEnv) (configuration.Worker, error) {
	return configuration.Worker{}, nil
}

func (automotive) Seed(context.Context, configuration.SeedEnv) error { return nil }
