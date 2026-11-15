package content

import (
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

	// Catalogs
	reg.RegisterCatalog(&CounterpartyRegistration{})
	reg.RegisterCatalog(&CurrencyRegistration{})
	reg.RegisterCatalog(&OrganizationRegistration{})
	reg.RegisterCatalog(&ContractRegistration{})

	reg.RegisterCatalog(&IntegrationAccountRegistration{})
	reg.RegisterCatalog(&IntegrationPoolRegistration{})
	reg.RegisterCatalog(&CryptoRPCEndpointRegistration{})

	// Crypto catalogs
	reg.RegisterCatalog(&BlockchainNetworkRegistration{})
	reg.RegisterCatalog(&TokenRegistration{})
	reg.RegisterCatalog(&MerchantRegistration{})
	reg.RegisterCatalog(&WalletRegistration{})
	reg.RegisterCatalog(&CryptoProviderAccountRegistration{})
	reg.RegisterCatalog(&RateSourceRegistration{})

	// Documents
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

	// Registers
	reg.RegisterRegister(&ExchangeRateRegisterRegistration{})

	for key, executor := range DatasetExecutors() {
		reg.RegisterDatasetExecutor(key, executor)
	}

	// Datasets — declarative, metadata-driven reports
	for _, ds := range AllDatasets() {
		reg.RegisterDataset(ds)
	}
}

// AllDatasets returns all built-in report dataset definitions.
// Decoupled from FactoryRegistry so that non-HTTP components (worker, scheduler)
// can build a Compiler without the HTTP routing layer.
func AllDatasets() []*schema.Dataset {
	return []*schema.Dataset{
		&DocumentJournalDataset,
	}
}

// HTTPContractBundles returns the configuration's wire contracts: the entity
// factory bundle (built-in content plus the given extension registrations) and
// the crypto route module bundle. The contract generator and route tests pass
// them to contracts.Registry and contracts.Routes.
func HTTPContractBundles(register ...func(*v1.FactoryRegistry)) []spec.Bundle {
	reg := v1.NewFactoryRegistry()
	RegisterDefaults(reg)
	for _, fn := range register {
		fn(reg)
	}
	return []spec.Bundle{reg.HTTPContracts(), cryptohttp.HTTPContracts()}
}
