package content

import (
	v1 "metapus/internal/infrastructure/http/v1"

	"metapus/internal/domain/reports/schema"
)

// RegisterDefaults populates the registry with all built-in (core) entity factories.
// This is the "business content" layer — specific entities shipped with Metapus.
//
// Client extensions call RegisterDefaults(reg) first, then add their own:
//
//	reg := v1.NewFactoryRegistry()
//	content.RegisterDefaults(reg)
//	reg.RegisterCatalog(&custom.CustomCatalogRegistration{})
func RegisterDefaults(reg *v1.FactoryRegistry) {
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
	reg.RegisterDocument(&CryptoInvoiceRegistration{})
	reg.RegisterDocument(&CryptoPaymentRegistration{})
	reg.RegisterDocument(&CryptoWithdrawalRegistration{})
	reg.RegisterDocument(&CryptoSweepRegistration{})
	reg.RegisterDocument(&CryptoProviderBalanceOperationRegistration{})
	reg.RegisterDocument(&CryptoResourceRentalRegistration{})
	reg.RegisterDocument(&CryptoNetworkOperationRegistration{})
	reg.RegisterDocument(&TreasuryWithdrawalRegistration{})
	reg.RegisterCustomDocument(&WithdrawalRequestRegistration{})

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
