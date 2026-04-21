package content

import (
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/storage/postgres/report_repo"

	"metapus/internal/domain/reports"
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

	// Reports (typed — platform auto-wires handler + metadata + permission)
	reportRepo := report_repo.NewReportRepo()
	v1.RegisterTypedReport(reg, reports.NewStockBalanceExecutor(reportRepo))
	v1.RegisterTypedReport(reg, reports.NewStockTurnoverExecutor(reportRepo))
	v1.RegisterTypedReport(reg, reports.NewDocumentJournalExecutor(reportRepo))
}
