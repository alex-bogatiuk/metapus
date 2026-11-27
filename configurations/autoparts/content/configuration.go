// Package content assembles the add-on «Автозапчасти» (autoparts): the auto
// parts store's specifics on the base configuration «Торговля и сервис»
// (configurations/trade). The add-on works with that base only: it imports
// the base's packages and the platform's public API (pkg/sdk), and the
// platform runs the base first and the add-on on top of it.
//
// The add-on adds nothing yet: a tenant of it is a trade tenant that will
// get the auto parts objects and fields as they come.
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
const Name = "autoparts"

// AutoParts returns the add-on «Автозапчасти».
func AutoParts() configuration.AddOn { return autoparts{} }

type autoparts struct{}

func (autoparts) Name() string { return Name }

// APIVersion: the add-on is built for the platform API 3.8.
func (autoparts) APIVersion() string { return "3.8.0" }

func (autoparts) Base() configuration.Configuration { return trade.Trade() }

// BaseVersion: the add-on is built for «Торговля и сервис» 1.0.
func (autoparts) BaseVersion() string { return "1.0.0" }

func (autoparts) Register(*registry.Registry) {}

func (autoparts) RouteContracts() []contracts.Bundle { return nil }

// Migrations: the add-on has no tables yet.
func (autoparts) Migrations() fs.FS { return nil }

func (autoparts) IntegrationCatalog() *integrations.Catalog { return nil }

func (autoparts) PoolConsumers() []integrations.PoolConsumer { return nil }

func (autoparts) Server(context.Context, configuration.ServerEnv) (configuration.Server, error) {
	return configuration.Server{}, nil
}

func (autoparts) Worker(context.Context, configuration.WorkerEnv) (configuration.Worker, error) {
	return configuration.Worker{}, nil
}

func (autoparts) Seed(context.Context, configuration.SeedEnv) error { return nil }
