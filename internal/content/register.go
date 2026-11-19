package content

import (
	"metapus/internal/core/entity"
	"metapus/internal/domain/auth"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/infrastructure/http/cryptohttp"
	v1 "metapus/internal/infrastructure/http/v1"

	"metapus/internal/domain/reports/schema"
)

// RegisterDefaults populates the registry with all built-in (core) entity factories.
// This is the "business content" layer — specific entities shipped with Metapus.
//
// Client extensions call RegisterDefaults(reg) first, then add their own:
//
//	reg := v1.NewFactoryRegistry()
//	content.RegisterDefaults(reg, content.Services{...})
//	reg.RegisterCatalog(&custom.CustomCatalogRegistration{})
//
// Services is optional; contract generation and tests register without it.
func RegisterDefaults(reg *v1.FactoryRegistry, services ...Services) {
	var svc Services
	if len(services) > 0 {
		svc = services[0]
	}

	RegisterPlatform(reg)
	registerCryptoEntities(reg, svc)

	for key, executor := range DatasetExecutors() {
		reg.RegisterDatasetExecutor(key, executor)
	}

	// Datasets — declarative, metadata-driven reports
	for _, ds := range AllDatasets() {
		reg.RegisterDataset(ds)
	}
}

// RegisterPlatform registers the platform's applied entities: the shared
// catalogs, integrations and exchange rates that any configuration uses.
func RegisterPlatform(reg *v1.FactoryRegistry) {
	reg.RegisterCatalog(counterpartyRegistration())
	reg.RegisterCatalog(currencyRegistration())
	reg.RegisterCatalog(organizationRegistration())
	reg.RegisterCatalog(contractRegistration())
	reg.RegisterCatalog(&IntegrationAccountRegistration{})
	reg.RegisterCatalog(&IntegrationPoolRegistration{})
	reg.RegisterCatalog(&RateSourceRegistration{})
	reg.RegisterRegister(&ExchangeRateRegisterRegistration{})
}

// registerCryptoEntities registers the crypto-processing configuration's
// entities.
func registerCryptoEntities(reg *v1.FactoryRegistry, svc Services) {
	reg.RegisterCatalog(&CryptoRPCEndpointRegistration{})
	reg.RegisterCatalog(&BlockchainNetworkRegistration{})
	reg.RegisterCatalog(tokenRegistration())
	reg.RegisterCatalog(&MerchantRegistration{})
	reg.RegisterCatalog(&WalletRegistration{})
	reg.RegisterCatalog(&CryptoProviderAccountRegistration{})

	reg.RegisterDocument(&CryptoInvoiceRegistration{services: svc})
	reg.RegisterDocument(&CryptoPaymentRegistration{})
	reg.RegisterDocument(&CryptoWithdrawalRegistration{})
	reg.RegisterDocument(&CryptoSweepRegistration{})
	reg.RegisterDocument(&CryptoProviderBalanceOperationRegistration{})
	reg.RegisterDocument(&CryptoResourceRentalRegistration{})
	reg.RegisterDocument(&CryptoNetworkOperationRegistration{})
	reg.RegisterDocument(&TreasuryWithdrawalRegistration{services: svc})
	reg.RegisterDocument(&CryptoCustodyReceiptRegistration{})
	reg.RegisterDocument(&CryptoMerchantCreditRegistration{})
	reg.RegisterDocument(&CryptoPlatformFundingRegistration{})
	reg.RegisterCustomDocument(&WithdrawalRequestRegistration{services: svc})
}

// Compose assembles the application's factory registry: the built-in
// content, then each client extension (extensions.Register). The server and
// the worker both start from it, so a dataset an extension registers is
// served over HTTP and run by scheduled reports alike.
func Compose(svc Services, register ...func(*v1.FactoryRegistry)) *v1.FactoryRegistry {
	reg := v1.NewFactoryRegistry()
	RegisterDefaults(reg, svc)
	for _, fn := range register {
		fn(reg)
	}
	return reg
}

// AllDatasets returns all built-in report dataset definitions. Processes
// that run reports take them from Compose, which adds the extensions'.
func AllDatasets() []*schema.Dataset {
	return []*schema.Dataset{
		&DocumentJournalDataset,
	}
}

// Permissions returns every permission the assembled application's entities
// and reports require (v1.FactoryRegistry.Permissions): the built-in content
// and the given client extensions, followed by the platform's system
// administration permissions (auth.SystemPermissions). Migration runs sync
// them into each tenant.
func Permissions(register ...func(*v1.FactoryRegistry)) []auth.Permission {
	return append(Compose(Services{}, register...).Permissions(), auth.SystemPermissions()...)
}

// CatalogDefinitions returns the Definitions of every registered catalog,
// built-in and of client extensions; tenant migrations number their items
// stored without a code (migration.WithCatalogCodes).
func CatalogDefinitions(register ...func(*v1.FactoryRegistry)) []entity.Definition {
	reg := Compose(Services{}, register...)
	definitions := make([]entity.Definition, 0, len(reg.Catalogs()))
	for _, catalog := range reg.Catalogs() {
		definitions = append(definitions, catalog.EntityDefinition())
	}
	return definitions
}

// HTTPContractBundles returns every wire contract of the application: the
// platform's (with client extensions) followed by the crypto configuration's.
// Route checks of the assembled server use it.
func HTTPContractBundles(register ...func(*v1.FactoryRegistry)) []spec.Bundle {
	return append(PlatformHTTPContractBundles(register...), CryptoHTTPContractBundles()...)
}

// PlatformHTTPContractBundles are the platform entities' contracts and those
// of client extensions, which use the platform's generic UI. The generator
// writes them to the platform's frontend files.
func PlatformHTTPContractBundles(register ...func(*v1.FactoryRegistry)) []spec.Bundle {
	reg := v1.NewFactoryRegistry()
	RegisterPlatform(reg)
	for _, fn := range register {
		fn(reg)
	}
	return []spec.Bundle{reg.HTTPContracts()}
}

// CryptoHTTPContractBundles are the crypto configuration's contracts: its
// entities and its route module. The generator writes them to the
// configuration's own frontend files.
func CryptoHTTPContractBundles() []spec.Bundle {
	reg := v1.NewFactoryRegistry()
	registerCryptoEntities(reg, Services{})
	return []spec.Bundle{reg.HTTPContracts(), cryptohttp.HTTPContracts()}
}
