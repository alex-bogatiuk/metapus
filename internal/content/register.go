package content

import (
	v1 "metapus/internal/infrastructure/http/v1"
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

	// Documents
	reg.RegisterDocument(&GoodsReceiptRegistration{})
	reg.RegisterDocument(&GoodsIssueRegistration{})

	// Registers
	reg.RegisterRegister(&StockRegisterRegistration{})

	// Datasets — declarative, metadata-driven reports (replaces legacy RegisterTypedReport)
	reg.RegisterDataset(&StockBalanceDataset)
	reg.RegisterDataset(&StockTurnoverDataset)
	reg.RegisterDataset(&DocumentJournalDataset)
}

