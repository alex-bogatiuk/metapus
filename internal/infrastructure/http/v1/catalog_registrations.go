// Package v1 — catalog_registrations.go contains concrete CatalogRegistration
// implementations for all built-in catalog types.
// This is the "business content" layer — specific catalogs shipped with Metapus.
// Core interfaces (CatalogRegistration, CatalogDeps) live in catalog_factory.go.
package v1

import (
	"context"

	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/contract"
	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/domain/catalogs/nomenclature"
	"metapus/internal/domain/catalogs/organization"
	"metapus/internal/domain/catalogs/unit"
	"metapus/internal/domain/catalogs/vat_rate"
	"metapus/internal/domain/catalogs/warehouse"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/metadata"
)

// ---------------------------------------------------------------------------
// Concrete factories (business content)
// ---------------------------------------------------------------------------

// CounterpartyRegistration wires the Counterparty catalog type.
type CounterpartyRegistration struct{}

func (r *CounterpartyRegistration) RoutePrefix() string      { return "counterparties" }
func (r *CounterpartyRegistration) Permission() string       { return "catalog:counterparty" }
func (r *CounterpartyRegistration) ReferenceTypes() []string { return []string{"supplier", "customer"} }
func (r *CounterpartyRegistration) EntityName() string       { return "Counterparty" }
func (r *CounterpartyRegistration) EntityLabel() string      { return "Контрагенты" }
func (r *CounterpartyRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Контрагент",
		Plural:   "Контрагенты",
		NewLabel: "Новый контрагент",
		Genitive: "контрагента",
	}
}
func (r *CounterpartyRegistration) EntityStruct() interface{} { return counterparty.Counterparty{} }

func (r *CounterpartyRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewCounterpartyRepo()
	service := counterparty.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "counterparty", deps.EventWriter)
	return handlers.NewCounterpartyHandler(deps.BaseHandler, service)
}

// NomenclatureRegistration wires the Nomenclature catalog type.
type NomenclatureRegistration struct{}

func (r *NomenclatureRegistration) RoutePrefix() string      { return "nomenclature" }
func (r *NomenclatureRegistration) Permission() string       { return "catalog:nomenclature" }
func (r *NomenclatureRegistration) ReferenceTypes() []string { return []string{"product"} }
func (r *NomenclatureRegistration) EntityName() string       { return "Nomenclature" }
func (r *NomenclatureRegistration) EntityLabel() string      { return "Номенклатура" }
func (r *NomenclatureRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Номенклатура",
		Plural:   "Номенклатура",
		NewLabel: "Новая номенклатура",
		Genitive: "номенклатуры",
	}
}
func (r *NomenclatureRegistration) EntityStruct() interface{} { return nomenclature.Nomenclature{} }

func (r *NomenclatureRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewNomenclatureRepo()
	service := nomenclature.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "nomenclature", deps.EventWriter)
	return handlers.NewNomenclatureHandler(deps.BaseHandler, service)
}

// WarehouseRegistration wires the Warehouse catalog type.
type WarehouseRegistration struct{}

func (r *WarehouseRegistration) RoutePrefix() string      { return "warehouses" }
func (r *WarehouseRegistration) Permission() string       { return "catalog:warehouse" }
func (r *WarehouseRegistration) ReferenceTypes() []string { return []string{"warehouse"} }
func (r *WarehouseRegistration) EntityName() string       { return "Warehouse" }
func (r *WarehouseRegistration) EntityLabel() string      { return "Склады" }
func (r *WarehouseRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Склад",
		Plural:   "Склады",
		NewLabel: "Новый склад",
		Genitive: "склада",
	}
}
func (r *WarehouseRegistration) EntityStruct() interface{} { return warehouse.Warehouse{} }

func (r *WarehouseRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewWarehouseRepo()
	service := warehouse.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "warehouse", deps.EventWriter)
	return handlers.NewWarehouseHandler(deps.BaseHandler, service)
}

// UnitRegistration wires the Unit catalog type.
type UnitRegistration struct{}

func (r *UnitRegistration) RoutePrefix() string      { return "units" }
func (r *UnitRegistration) Permission() string       { return "catalog:unit" }
func (r *UnitRegistration) ReferenceTypes() []string { return []string{"unit"} }
func (r *UnitRegistration) EntityName() string       { return "Unit" }
func (r *UnitRegistration) EntityLabel() string      { return "Единицы измерения" }
func (r *UnitRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Единица измерения",
		Plural:   "Единицы измерения",
		NewLabel: "Новая единица измерения",
		Genitive: "единицы измерения",
	}
}
func (r *UnitRegistration) EntityStruct() interface{} { return unit.Unit{} }

func (r *UnitRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewUnitRepo()
	service := unit.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "unit", deps.EventWriter)
	return handlers.NewUnitHandler(deps.BaseHandler, service)
}

// CurrencyRegistration wires the Currency catalog type.
type CurrencyRegistration struct{}

func (r *CurrencyRegistration) RoutePrefix() string      { return "currencies" }
func (r *CurrencyRegistration) Permission() string       { return "catalog:currency" }
func (r *CurrencyRegistration) ReferenceTypes() []string { return []string{"currency"} }
func (r *CurrencyRegistration) EntityName() string       { return "Currency" }
func (r *CurrencyRegistration) EntityLabel() string      { return "Валюты" }
func (r *CurrencyRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Валюта",
		Plural:   "Валюты",
		NewLabel: "Новая валюта",
		Genitive: "валюты",
	}
}
func (r *CurrencyRegistration) EntityStruct() interface{} { return currency.Currency{} }

func (r *CurrencyRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewCurrencyRepo()
	service := currency.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "currency", deps.EventWriter)
	return handlers.NewCurrencyHandler(deps.BaseHandler, service)
}

// OrganizationRegistration wires the Organization catalog type.
type OrganizationRegistration struct{}

func (r *OrganizationRegistration) RoutePrefix() string      { return "organizations" }
func (r *OrganizationRegistration) Permission() string       { return "catalog:organization" }
func (r *OrganizationRegistration) ReferenceTypes() []string { return []string{"organization"} }
func (r *OrganizationRegistration) EntityName() string       { return "Organization" }
func (r *OrganizationRegistration) EntityLabel() string      { return "Организации" }
func (r *OrganizationRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Организация",
		Plural:   "Организации",
		NewLabel: "Новая организация",
		Genitive: "организации",
	}
}
func (r *OrganizationRegistration) EntityStruct() interface{} { return organization.Organization{} }

func (r *OrganizationRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewOrganizationRepo()
	service := organization.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "organization", deps.EventWriter)

	// Invalidate CurrencyResolver cache when org's base currency changes
	if deps.CurrencyCacheInvalidator != nil {
		inv := deps.CurrencyCacheInvalidator
		service.Hooks().OnAfterUpdate(func(_ context.Context, org *organization.Organization) error {
			inv.InvalidateOrgCurrency(org.ID)
			return nil
		})
	}

	return handlers.NewOrganizationHandler(deps.BaseHandler, service)
}

// VATRateRegistration wires the VATRate catalog type.
type VATRateRegistration struct{}

func (r *VATRateRegistration) RoutePrefix() string      { return "vat-rates" }
func (r *VATRateRegistration) Permission() string       { return "catalog:vat_rate" }
func (r *VATRateRegistration) ReferenceTypes() []string { return []string{"vatrate"} }
func (r *VATRateRegistration) EntityName() string       { return "VATRate" }
func (r *VATRateRegistration) EntityLabel() string      { return "Ставки НДС" }
func (r *VATRateRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Ставка НДС",
		Plural:   "Ставки НДС",
		NewLabel: "Новая ставка НДС",
		Genitive: "ставки НДС",
	}
}
func (r *VATRateRegistration) EntityStruct() interface{} { return vat_rate.VATRate{} }

func (r *VATRateRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewVATRateRepo()
	service := vat_rate.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "vat_rate", deps.EventWriter)
	return handlers.NewVATRateHandler(deps.BaseHandler, service)
}

// ContractRegistration wires the Contract catalog type.
type ContractRegistration struct{}

func (r *ContractRegistration) RoutePrefix() string      { return "contracts" }
func (r *ContractRegistration) Permission() string       { return "catalog:contract" }
func (r *ContractRegistration) ReferenceTypes() []string { return []string{"contract"} }
func (r *ContractRegistration) EntityName() string       { return "Contract" }
func (r *ContractRegistration) EntityLabel() string      { return "Договоры" }
func (r *ContractRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Договор",
		Plural:   "Договоры",
		NewLabel: "Новый договор",
		Genitive: "договора",
	}
}
func (r *ContractRegistration) EntityStruct() interface{} { return contract.Contract{} }

func (r *ContractRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewContractRepo()
	service := contract.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "contract", deps.EventWriter)

	// Invalidate CurrencyResolver cache when contract's currency changes
	if deps.CurrencyCacheInvalidator != nil {
		inv := deps.CurrencyCacheInvalidator
		service.Hooks().OnAfterUpdate(func(_ context.Context, c *contract.Contract) error {
			inv.InvalidateContractCurrency(c.ID)
			return nil
		})
	}

	return handlers.NewContractHandler(deps.BaseHandler, service)
}
