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
//	reg.RegisterCatalog(&custom.VehicleRegistration{})
func RegisterDefaults(reg *v1.FactoryRegistry) {
	// Catalogs (order matches original registration order)
	reg.RegisterCatalog(&CounterpartyRegistration{})
	reg.RegisterCatalog(&NomenclatureRegistration{})
	reg.RegisterCatalog(&WarehouseRegistration{})
	reg.RegisterCatalog(&UnitRegistration{})
	reg.RegisterCatalog(&CurrencyRegistration{})
	reg.RegisterCatalog(&OrganizationRegistration{})
	reg.RegisterCatalog(&VATRateRegistration{})
	reg.RegisterCatalog(&ContractRegistration{})

	// Crypto catalogs
	reg.RegisterCatalog(&BlockchainNetworkRegistration{})
	reg.RegisterCatalog(&TokenRegistration{})
	reg.RegisterCatalog(&MerchantRegistration{})
	reg.RegisterCatalog(&WalletRegistration{})

	// Documents
	reg.RegisterDocument(&GoodsReceiptRegistration{})
	reg.RegisterDocument(&GoodsIssueRegistration{})
	reg.RegisterDocument(&CryptoInvoiceRegistration{})
	reg.RegisterDocument(&CryptoPaymentRegistration{})
	reg.RegisterDocument(&CryptoWithdrawalRegistration{})
	reg.RegisterDocument(&CryptoSweepRegistration{})

	// Registers
	reg.RegisterRegister(&StockRegisterRegistration{})

	// Datasets — declarative, metadata-driven reports (replaces legacy RegisterTypedReport)
	for _, ds := range AllDatasets() {
		reg.RegisterDataset(ds)
	}
}

// AllDatasets returns all built-in report dataset definitions.
// Decoupled from FactoryRegistry so that non-HTTP components (worker, scheduler)
// can build a Compiler without the HTTP routing layer.
func AllDatasets() []*schema.Dataset {
	return []*schema.Dataset{
		&StockBalanceDataset,
		&StockTurnoverDataset,
		&DocumentJournalDataset,
	}
}

