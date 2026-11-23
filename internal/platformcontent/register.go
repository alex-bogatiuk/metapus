// Package platformcontent registers the platform's shared applied entities:
// counterparties, currencies, organizations, contracts, integration accounts
// and pools, exchange-rate sources and the exchange-rate register. Every
// configuration builds on them; configuration.Compose registers them before
// the configuration's own entities.
package platformcontent

import (
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
)

// Integrations is what the integration catalogs take from the configuration.
type Integrations struct {
	// Catalog lists the application's integration kinds and providers; nil
	// is the platform's alone (integrations.PlatformCatalog).
	Catalog *integrations.Catalog
	// PoolConsumers are the configuration's catalogs that read accounts
	// through integration pools.
	PoolConsumers []integrations.PoolConsumer
}

// Register adds the shared entities to reg.
func Register(reg *v1.FactoryRegistry, with Integrations) {
	catalog := with.Catalog
	if catalog == nil {
		catalog = integrations.PlatformCatalog()
	}
	registerProviderEnum(catalog)

	reg.RegisterCatalog(counterpartyRegistration())
	reg.RegisterCatalog(currencyRegistration())
	reg.RegisterCatalog(organizationRegistration())
	reg.RegisterCatalog(contractRegistration())
	reg.RegisterCatalog(&IntegrationAccountRegistration{catalog: catalog, consumers: with.PoolConsumers})
	reg.RegisterCatalog(&IntegrationPoolRegistration{catalog: catalog, consumers: with.PoolConsumers})
	reg.RegisterCatalog(&RateSourceRegistration{})
	reg.RegisterRegister(&ExchangeRateRegisterRegistration{})
}
