// Package v1 provides HTTP API version 1.
package v1

import (
	"metapus/internal/core/numerator"
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
)

// CatalogDeps holds shared dependencies injected into every catalog factory.
type CatalogDeps struct {
	BaseHandler *handlers.BaseHandler
	Numerator   numerator.Generator
}

// CatalogRegistration is the Abstract Factory interface for catalog types.
// Mirrors DocumentRegistration for catalogs, enabling auto-registration
// of routes and metadata from a single declaration.
//
// Adding a new catalog type:
//  1. Create model, repo, service (embed CatalogService).
//  2. Create handler (CatalogHandler[T, CreateDTO, UpdateDTO]).
//  3. Implement CatalogRegistration.
//  4. Append to catalogFactories slice below.
type CatalogRegistration interface {
	// RoutePrefix returns the URL path segment, e.g. "counterparties".
	RoutePrefix() string
	// Permission returns the permission prefix, e.g. "catalog:counterparty".
	Permission() string
	// ReferenceTypes returns ref types this catalog satisfies,
	// e.g. ["supplier", "customer"] for Counterparty.
	ReferenceTypes() []string
	// EntityName returns the metadata registry name, e.g. "Counterparty".
	EntityName() string
	// EntityLabel returns the human-readable entity label, e.g. "Контрагенты".
	EntityLabel() string
	// EntityStruct returns a zero-value instance for metadata.Inspect().
	EntityStruct() interface{}
	// Build creates repo, service, and handler.
	Build(deps CatalogDeps) CatalogRouteHandler
}

// catalogFactories is the registry of all catalog types.
// To add a new catalog, append its factory here.
var catalogFactories = []CatalogRegistration{
	&CounterpartyRegistration{},
	&NomenclatureRegistration{},
	&WarehouseRegistration{},
	&UnitRegistration{},
	&CurrencyRegistration{},
	&OrganizationRegistration{},
	&VATRateRegistration{},
	&ContractRegistration{},
}

// ---------------------------------------------------------------------------
// Concrete factories
// ---------------------------------------------------------------------------

// CounterpartyRegistration wires the Counterparty catalog type.
type CounterpartyRegistration struct{}

func (r *CounterpartyRegistration) RoutePrefix() string     { return "counterparties" }
func (r *CounterpartyRegistration) Permission() string      { return "catalog:counterparty" }
func (r *CounterpartyRegistration) ReferenceTypes() []string { return []string{"supplier", "customer"} }
func (r *CounterpartyRegistration) EntityName() string      { return "Counterparty" }
func (r *CounterpartyRegistration) EntityLabel() string     { return "Контрагенты" }
func (r *CounterpartyRegistration) EntityStruct() interface{} { return counterparty.Counterparty{} }

func (r *CounterpartyRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewCounterpartyRepo()
	service := counterparty.NewService(repo, deps.Numerator)
	return handlers.NewCounterpartyHandler(deps.BaseHandler, service)
}

// NomenclatureRegistration wires the Nomenclature catalog type.
type NomenclatureRegistration struct{}

func (r *NomenclatureRegistration) RoutePrefix() string     { return "nomenclature" }
func (r *NomenclatureRegistration) Permission() string      { return "catalog:nomenclature" }
func (r *NomenclatureRegistration) ReferenceTypes() []string { return []string{"product"} }
func (r *NomenclatureRegistration) EntityName() string      { return "Nomenclature" }
func (r *NomenclatureRegistration) EntityLabel() string     { return "Номенклатура" }
func (r *NomenclatureRegistration) EntityStruct() interface{} { return nomenclature.Nomenclature{} }

func (r *NomenclatureRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewNomenclatureRepo()
	service := nomenclature.NewService(repo, deps.Numerator)
	return handlers.NewNomenclatureHandler(deps.BaseHandler, service)
}

// WarehouseRegistration wires the Warehouse catalog type.
type WarehouseRegistration struct{}

func (r *WarehouseRegistration) RoutePrefix() string     { return "warehouses" }
func (r *WarehouseRegistration) Permission() string      { return "catalog:warehouse" }
func (r *WarehouseRegistration) ReferenceTypes() []string { return []string{"warehouse"} }
func (r *WarehouseRegistration) EntityName() string      { return "Warehouse" }
func (r *WarehouseRegistration) EntityLabel() string     { return "Склады" }
func (r *WarehouseRegistration) EntityStruct() interface{} { return warehouse.Warehouse{} }

func (r *WarehouseRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewWarehouseRepo()
	service := warehouse.NewService(repo, deps.Numerator)
	return handlers.NewWarehouseHandler(deps.BaseHandler, service)
}

// UnitRegistration wires the Unit catalog type.
type UnitRegistration struct{}

func (r *UnitRegistration) RoutePrefix() string     { return "units" }
func (r *UnitRegistration) Permission() string      { return "catalog:unit" }
func (r *UnitRegistration) ReferenceTypes() []string { return []string{"unit"} }
func (r *UnitRegistration) EntityName() string      { return "Unit" }
func (r *UnitRegistration) EntityLabel() string     { return "Единицы измерения" }
func (r *UnitRegistration) EntityStruct() interface{} { return unit.Unit{} }

func (r *UnitRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewUnitRepo()
	service := unit.NewService(repo, deps.Numerator)
	return handlers.NewUnitHandler(deps.BaseHandler, service)
}

// CurrencyRegistration wires the Currency catalog type.
type CurrencyRegistration struct{}

func (r *CurrencyRegistration) RoutePrefix() string     { return "currencies" }
func (r *CurrencyRegistration) Permission() string      { return "catalog:currency" }
func (r *CurrencyRegistration) ReferenceTypes() []string { return []string{"currency"} }
func (r *CurrencyRegistration) EntityName() string      { return "Currency" }
func (r *CurrencyRegistration) EntityLabel() string     { return "Валюты" }
func (r *CurrencyRegistration) EntityStruct() interface{} { return currency.Currency{} }

func (r *CurrencyRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewCurrencyRepo()
	service := currency.NewService(repo, deps.Numerator)
	return handlers.NewCurrencyHandler(deps.BaseHandler, service)
}

// OrganizationRegistration wires the Organization catalog type.
type OrganizationRegistration struct{}

func (r *OrganizationRegistration) RoutePrefix() string     { return "organizations" }
func (r *OrganizationRegistration) Permission() string      { return "catalog:organization" }
func (r *OrganizationRegistration) ReferenceTypes() []string { return []string{"organization"} }
func (r *OrganizationRegistration) EntityName() string      { return "Organization" }
func (r *OrganizationRegistration) EntityLabel() string     { return "Организации" }
func (r *OrganizationRegistration) EntityStruct() interface{} { return organization.Organization{} }

func (r *OrganizationRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewOrganizationRepo()
	service := organization.NewService(repo, deps.Numerator)
	return handlers.NewOrganizationHandler(deps.BaseHandler, service)
}

// VATRateRegistration wires the VATRate catalog type.
type VATRateRegistration struct{}

func (r *VATRateRegistration) RoutePrefix() string     { return "vat-rates" }
func (r *VATRateRegistration) Permission() string      { return "catalog:vat_rate" }
func (r *VATRateRegistration) ReferenceTypes() []string { return []string{"vatrate"} }
func (r *VATRateRegistration) EntityName() string      { return "VATRate" }
func (r *VATRateRegistration) EntityLabel() string     { return "Ставки НДС" }
func (r *VATRateRegistration) EntityStruct() interface{} { return vat_rate.VATRate{} }

func (r *VATRateRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewVATRateRepo()
	service := vat_rate.NewService(repo, deps.Numerator)
	return handlers.NewVATRateHandler(deps.BaseHandler, service)
}

// ContractRegistration wires the Contract catalog type.
type ContractRegistration struct{}

func (r *ContractRegistration) RoutePrefix() string     { return "contracts" }
func (r *ContractRegistration) Permission() string      { return "catalog:contract" }
func (r *ContractRegistration) ReferenceTypes() []string { return []string{"contract"} }
func (r *ContractRegistration) EntityName() string      { return "Contract" }
func (r *ContractRegistration) EntityLabel() string     { return "Договоры" }
func (r *ContractRegistration) EntityStruct() interface{} { return contract.Contract{} }

func (r *ContractRegistration) Build(deps CatalogDeps) CatalogRouteHandler {
	repo := catalog_repo.NewContractRepo()
	service := contract.NewService(repo, deps.Numerator)
	return handlers.NewContractHandler(deps.BaseHandler, service)
}
