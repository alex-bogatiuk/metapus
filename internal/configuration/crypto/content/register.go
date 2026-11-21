package content

import (
	v1 "metapus/internal/infrastructure/http/v1"

	"metapus/internal/domain/reports/schema"
)

// register adds the crypto configuration's entities and reports: its
// catalogs and documents, the document journal and its executor. svc may be
// the zero value; contract generation and tests register without it.
func register(reg *v1.FactoryRegistry, svc Services) {
	registerCryptoEntities(reg, svc)
	for key, executor := range DatasetExecutors() {
		reg.RegisterDatasetExecutor(key, executor)
	}
	for _, ds := range AllDatasets() {
		reg.RegisterDataset(ds)
	}
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

// AllDatasets returns all built-in report dataset definitions. Processes
// that run reports take them from configuration.Compose, which adds the
// extensions'.
func AllDatasets() []*schema.Dataset {
	return []*schema.Dataset{
		&DocumentJournalDataset,
	}
}
