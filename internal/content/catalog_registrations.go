// Package content contains concrete Registration implementations for all
// built-in entity types shipped with Metapus ("business content" layer).
//
// Platform Core interfaces live in internal/platform/ and v1/.
// Client extensions DO NOT modify this package — they register their own
// entities via FactoryRegistry from their own Go module.
package content

import (
	"context"

	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/blockchain_network"
	"metapus/internal/domain/catalogs/contract"
	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/catalogs/nomenclature"
	"metapus/internal/domain/catalogs/organization"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/catalogs/unit"
	"metapus/internal/domain/catalogs/vat_rate"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/catalogs/warehouse"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

func init() {
	// Register Enum metadata globally for automatic filter UI dropdown resolution.

	// Nomenclature Types
	metadata.RegisterEnum[nomenclature.NomenclatureType]([]metadata.EnumValue{
		{Value: "goods", Label: "Товар"},
		{Value: "service", Label: "Услуга"},
		{Value: "work", Label: "Работа"},
		{Value: "material", Label: "Материал"},
		{Value: "semi", Label: "Полуфабрикат"},
		{Value: "product", Label: "Продукция"},
	})

	// Counterparty Types
	metadata.RegisterEnum[counterparty.CounterpartyType]([]metadata.EnumValue{
		{Value: "customer", Label: "Покупатель"},
		{Value: "supplier", Label: "Поставщик"},
		{Value: "both", Label: "Покупатель и Поставщик"},
		{Value: "other", Label: "Прочие"},
	})

	// Legal Forms
	metadata.RegisterEnum[counterparty.LegalForm]([]metadata.EnumValue{
		{Value: "individual", Label: "Физлицо"},
		{Value: "sole_trader", Label: "ИП"},
		{Value: "company", Label: "Юрлицо"},
		{Value: "government", Label: "Гос. орган"},
	})

	// Warehouse Types
	metadata.RegisterEnum[warehouse.WarehouseType]([]metadata.EnumValue{
		{Value: "main", Label: "Основной"},
		{Value: "distribution", Label: "Распределительный"},
		{Value: "retail", Label: "Розничный"},
		{Value: "production", Label: "Производственный"},
		{Value: "transit", Label: "Транзитный"},
	})

	// Unit Types
	metadata.RegisterEnum[unit.UnitType]([]metadata.EnumValue{
		{Value: "piece", Label: "Штуки"},
		{Value: "weight", Label: "Вес"},
		{Value: "length", Label: "Длина"},
		{Value: "area", Label: "Площадь"},
		{Value: "volume", Label: "Объем"},
		{Value: "time", Label: "Время"},
		{Value: "pack", Label: "Упаковки"},
	})

	// Contract Types
	metadata.RegisterEnum[contract.ContractType]([]metadata.EnumValue{
		{Value: "supply", Label: "С поставщиком"},
		{Value: "sale", Label: "С покупателем"},
		{Value: "other", Label: "Прочее"},
	})

	// Organization Tax Systems
	metadata.RegisterEnum[organization.TaxSystem]([]metadata.EnumValue{
		{Value: "osno", Label: "ОСНО"},
		{Value: "usn_income", Label: "УСН (доходы)"},
		{Value: "usn_income_expense", Label: "УСН (доходы − расходы)"},
		{Value: "envd", Label: "ЕНВД"},
		{Value: "patent", Label: "Патент"},
	})

	// Organization Inventory Methods
	metadata.RegisterEnum[organization.InventoryMethod]([]metadata.EnumValue{
		{Value: "fifo", Label: "ФИФО"},
		{Value: "average", Label: "Средняя"},
		{Value: "specific", Label: "По партиям"},
	})

	// Merchant KYB Status
	metadata.RegisterEnum[merchant.KYBStatus]([]metadata.EnumValue{
		{Value: "pending", Label: "Ожидает верификации"},
		{Value: "approved", Label: "Верифицирован"},
		{Value: "rejected", Label: "Отклонён"},
	})

	// Wallet Tier
	metadata.RegisterEnum[wallet.WalletTier]([]metadata.EnumValue{
		{Value: "pool", Label: "Pool"},
		{Value: "hot", Label: "Hot"},
		{Value: "warm", Label: "Warm"},
		{Value: "cold", Label: "Cold"},
	})

	// Wallet Status
	metadata.RegisterEnum[wallet.WalletStatus]([]metadata.EnumValue{
		{Value: "free", Label: "Свободен"},
		{Value: "leased", Label: "Арендован"},
		{Value: "assigned", Label: "Назначен"},
		{Value: "sweep_pending", Label: "Ожидает sweep"},
		{Value: "frozen", Label: "Заморожен"},
	})

	// Wallet Allocation Mode
	metadata.RegisterEnum[wallet.AllocationMode]([]metadata.EnumValue{
		{Value: "transient", Label: "Временный (per-invoice)"},
		{Value: "persistent", Label: "Постоянный (per-customer)"},
	})
}

// ---------------------------------------------------------------------------
// Counterparty
// ---------------------------------------------------------------------------

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
func (r *CounterpartyRegistration) SearchableFields() platform.SearchFields {
	return platform.SearchFields{
		SearchCols:  []string{"name", "code", "inn", "phone", "email"},
		TitleCol:    "name",
		SubtitleCol: "code",
	}
}

func (r *CounterpartyRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewCounterpartyRepo()
	service := counterparty.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "counterparty", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*counterparty.Counterparty,
		dto.CreateCounterpartyRequest,
		dto.UpdateCounterpartyRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "counterparty",
		MapCreateDTO: func(req dto.CreateCounterpartyRequest) *counterparty.Counterparty { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateCounterpartyRequest, existing *counterparty.Counterparty) *counterparty.Counterparty {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *counterparty.Counterparty) any { return dto.FromCounterparty(entity) },
	})
}

// ---------------------------------------------------------------------------
// Nomenclature
// ---------------------------------------------------------------------------

type NomenclatureRegistration struct{}

func (r *NomenclatureRegistration) RoutePrefix() string      { return "nomenclatures" }
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

func (r *NomenclatureRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewNomenclatureRepo()
	service := nomenclature.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "nomenclature", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*nomenclature.Nomenclature,
		dto.CreateNomenclatureRequest,
		dto.UpdateNomenclatureRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "nomenclature",
		MapCreateDTO: func(req dto.CreateNomenclatureRequest) *nomenclature.Nomenclature { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateNomenclatureRequest, existing *nomenclature.Nomenclature) *nomenclature.Nomenclature {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *nomenclature.Nomenclature) any { return dto.FromNomenclature(entity) },
		ResolveRefs: resolveNomenclatureRefs,
		MapToDTOWithRefs: func(entity *nomenclature.Nomenclature, refs any) any {
			return dto.FromNomenclature(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

// ---------------------------------------------------------------------------
// Warehouse
// ---------------------------------------------------------------------------

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

func (r *WarehouseRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewWarehouseRepo()
	service := warehouse.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "warehouse", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*warehouse.Warehouse,
		dto.CreateWarehouseRequest,
		dto.UpdateWarehouseRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "warehouse",
		MapCreateDTO: func(req dto.CreateWarehouseRequest) *warehouse.Warehouse { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateWarehouseRequest, existing *warehouse.Warehouse) *warehouse.Warehouse {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *warehouse.Warehouse) any { return dto.FromWarehouse(entity) },
		ResolveRefs: resolveWarehouseRefs,
		MapToDTOWithRefs: func(entity *warehouse.Warehouse, refs any) any {
			return dto.FromWarehouse(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

// ---------------------------------------------------------------------------
// Unit
// ---------------------------------------------------------------------------

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

func (r *UnitRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewUnitRepo()
	service := unit.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "unit", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*unit.Unit,
		dto.CreateUnitRequest,
		dto.UpdateUnitRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "unit",
		MapCreateDTO: func(req dto.CreateUnitRequest) *unit.Unit { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateUnitRequest, existing *unit.Unit) *unit.Unit {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *unit.Unit) any { return dto.FromUnit(entity) },
		ResolveRefs: resolveUnitRefs,
		MapToDTOWithRefs: func(entity *unit.Unit, refs any) any {
			return dto.FromUnit(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

// ---------------------------------------------------------------------------
// Currency
// ---------------------------------------------------------------------------

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

func (r *CurrencyRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewCurrencyRepo()
	service := currency.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "currency", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*currency.Currency,
		dto.CreateCurrencyRequest,
		dto.UpdateCurrencyRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "currency",
		MapCreateDTO: func(req dto.CreateCurrencyRequest) *currency.Currency { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateCurrencyRequest, existing *currency.Currency) *currency.Currency {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *currency.Currency) any { return dto.FromCurrency(entity) },
	})
}

// ---------------------------------------------------------------------------
// Organization
// ---------------------------------------------------------------------------

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

func (r *OrganizationRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
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

	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*organization.Organization,
		dto.CreateOrganizationRequest,
		dto.UpdateOrganizationRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "organization",
		MapCreateDTO: func(req dto.CreateOrganizationRequest) *organization.Organization { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateOrganizationRequest, existing *organization.Organization) *organization.Organization {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *organization.Organization) any { return dto.FromOrganization(entity) },
		ResolveRefs: resolveOrganizationRefs,
		MapToDTOWithRefs: func(entity *organization.Organization, refs any) any {
			return dto.FromOrganization(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

// ---------------------------------------------------------------------------
// VATRate
// ---------------------------------------------------------------------------

type VATRateRegistration struct{}

func (r *VATRateRegistration) RoutePrefix() string      { return "vat-rates" }
func (r *VATRateRegistration) Permission() string       { return "catalog:vat_rate" }
func (r *VATRateRegistration) ReferenceTypes() []string { return []string{"vatrate", "vat_rate"} }
func (r *VATRateRegistration) TableName() string         { return "cat_vat_rates" }
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

func (r *VATRateRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewVATRateRepo()
	service := vat_rate.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "vat_rate", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*vat_rate.VATRate,
		dto.CreateVATRateRequest,
		dto.UpdateVATRateRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "vat_rate",
		MapCreateDTO: func(req dto.CreateVATRateRequest) *vat_rate.VATRate { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateVATRateRequest, existing *vat_rate.VATRate) *vat_rate.VATRate {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *vat_rate.VATRate) any { return dto.FromVATRate(entity) },
	})
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

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

func (r *ContractRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
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

	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*contract.Contract,
		dto.CreateContractRequest,
		dto.UpdateContractRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "contract",
		MapCreateDTO: func(req dto.CreateContractRequest) *contract.Contract { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateContractRequest, existing *contract.Contract) *contract.Contract {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *contract.Contract) any { return dto.FromContract(entity) },
		ResolveRefs: resolveContractRefs,
		MapToDTOWithRefs: func(entity *contract.Contract, refs any) any {
			return dto.FromContract(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

// ── ResolveRefs callbacks for catalogs with FK references ───────────────

func resolveCatalogRefs[T any](ctx context.Context, collect func(*postgres.ReferenceResolver, T), entities ...T) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, e := range entities {
		collect(resolver, e)
	}
	pool := tenant.MustGetPool(ctx)
	return resolver.Resolve(ctx, pool)
}

func resolveNomenclatureRefs(ctx context.Context, entities ...*nomenclature.Nomenclature) (any, error) {
	return resolveCatalogRefs(ctx, dto.CollectNomenclatureRefs, entities...)
}

func resolveOrganizationRefs(ctx context.Context, entities ...*organization.Organization) (any, error) {
	return resolveCatalogRefs(ctx, dto.CollectOrganizationRefs, entities...)
}

func resolveWarehouseRefs(ctx context.Context, entities ...*warehouse.Warehouse) (any, error) {
	return resolveCatalogRefs(ctx, dto.CollectWarehouseRefs, entities...)
}

func resolveUnitRefs(ctx context.Context, entities ...*unit.Unit) (any, error) {
	return resolveCatalogRefs(ctx, dto.CollectUnitRefs, entities...)
}

func resolveContractRefs(ctx context.Context, entities ...*contract.Contract) (any, error) {
	return resolveCatalogRefs(ctx, dto.CollectContractRefs, entities...)
}

// ---------------------------------------------------------------------------
// BlockchainNetwork
// ---------------------------------------------------------------------------

type BlockchainNetworkRegistration struct{}

func (r *BlockchainNetworkRegistration) RoutePrefix() string      { return "blockchain-networks" }
func (r *BlockchainNetworkRegistration) Permission() string       { return "catalog:blockchain_network" }
func (r *BlockchainNetworkRegistration) ReferenceTypes() []string { return []string{"blockchain_network"} }
func (r *BlockchainNetworkRegistration) EntityName() string       { return "BlockchainNetwork" }
func (r *BlockchainNetworkRegistration) EntityLabel() string      { return "Блокчейн-сети" }
func (r *BlockchainNetworkRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Блокчейн-сеть",
		Plural:   "Блокчейн-сети",
		NewLabel: "Новая блокчейн-сеть",
		Genitive: "блокчейн-сети",
	}
}
func (r *BlockchainNetworkRegistration) EntityStruct() interface{} {
	return blockchain_network.BlockchainNetwork{}
}

func (r *BlockchainNetworkRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewBlockchainNetworkRepo()
	service := blockchain_network.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "blockchain_network", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*blockchain_network.BlockchainNetwork,
		dto.CreateBlockchainNetworkRequest,
		dto.UpdateBlockchainNetworkRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "blockchain_network",
		MapCreateDTO: func(req dto.CreateBlockchainNetworkRequest) *blockchain_network.BlockchainNetwork {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateBlockchainNetworkRequest, existing *blockchain_network.BlockchainNetwork) *blockchain_network.BlockchainNetwork {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *blockchain_network.BlockchainNetwork) any {
			return dto.FromBlockchainNetwork(entity)
		},
	})
}

// ---------------------------------------------------------------------------
// Token
// ---------------------------------------------------------------------------

type TokenRegistration struct{}

func (r *TokenRegistration) RoutePrefix() string      { return "tokens" }
func (r *TokenRegistration) Permission() string       { return "catalog:token" }
func (r *TokenRegistration) ReferenceTypes() []string { return []string{"token"} }
func (r *TokenRegistration) EntityName() string       { return "Token" }
func (r *TokenRegistration) EntityLabel() string      { return "Токены" }
func (r *TokenRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Токен",
		Plural:   "Токены",
		NewLabel: "Новый токен",
		Genitive: "токена",
	}
}
func (r *TokenRegistration) EntityStruct() interface{} { return token.Token{} }

func (r *TokenRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewTokenRepo()
	service := token.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "token", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*token.Token,
		dto.CreateTokenRequest,
		dto.UpdateTokenRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "token",
		MapCreateDTO: func(req dto.CreateTokenRequest) *token.Token { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateTokenRequest, existing *token.Token) *token.Token {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *token.Token) any { return dto.FromToken(entity) },
		ResolveRefs: resolveTokenRefs,
		MapToDTOWithRefs: func(entity *token.Token, refs any) any {
			return dto.FromToken(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

func resolveTokenRefs(ctx context.Context, entities ...*token.Token) (any, error) {
	return resolveCatalogRefs(ctx, dto.CollectTokenRefs, entities...)
}

// ---------------------------------------------------------------------------
// Merchant
// ---------------------------------------------------------------------------

type MerchantRegistration struct{}

func (r *MerchantRegistration) RoutePrefix() string      { return "merchants" }
func (r *MerchantRegistration) Permission() string       { return "catalog:merchant" }
func (r *MerchantRegistration) ReferenceTypes() []string { return []string{"merchant"} }
func (r *MerchantRegistration) EntityName() string       { return "Merchant" }
func (r *MerchantRegistration) EntityLabel() string      { return "Мерчанты" }
func (r *MerchantRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Мерчант",
		Plural:   "Мерчанты",
		NewLabel: "Новый мерчант",
		Genitive: "мерчанта",
	}
}
func (r *MerchantRegistration) EntityStruct() interface{} { return merchant.Merchant{} }

func (r *MerchantRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewMerchantRepo()
	service := merchant.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "merchant", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*merchant.Merchant,
		dto.CreateMerchantRequest,
		dto.UpdateMerchantRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "merchant",
		MapCreateDTO: func(req dto.CreateMerchantRequest) *merchant.Merchant { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateMerchantRequest, existing *merchant.Merchant) *merchant.Merchant {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *merchant.Merchant) any { return dto.FromMerchant(entity) },
	})
}

// ---------------------------------------------------------------------------
// Wallet
// ---------------------------------------------------------------------------

type WalletRegistration struct{}

func (r *WalletRegistration) RoutePrefix() string      { return "wallets" }
func (r *WalletRegistration) Permission() string       { return "catalog:wallet" }
func (r *WalletRegistration) ReferenceTypes() []string { return []string{"wallet"} }
func (r *WalletRegistration) EntityName() string       { return "Wallet" }
func (r *WalletRegistration) EntityLabel() string      { return "Кошельки" }
func (r *WalletRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Кошелёк",
		Plural:   "Кошельки",
		NewLabel: "Новый кошелёк",
		Genitive: "кошелька",
	}
}
func (r *WalletRegistration) EntityStruct() interface{} { return wallet.Wallet{} }

func (r *WalletRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewWalletRepo()
	service := wallet.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "wallet", deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*wallet.Wallet,
		dto.CreateWalletRequest,
		dto.UpdateWalletRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "wallet",
		MapCreateDTO: func(req dto.CreateWalletRequest) *wallet.Wallet { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateWalletRequest, existing *wallet.Wallet) *wallet.Wallet {
			req.ApplyTo(existing); return existing
		},
		MapToDTO: func(entity *wallet.Wallet) any { return dto.FromWallet(entity) },
		ResolveRefs: resolveWalletRefs,
		MapToDTOWithRefs: func(entity *wallet.Wallet, refs any) any {
			return dto.FromWallet(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

func resolveWalletRefs(ctx context.Context, entities ...*wallet.Wallet) (any, error) {
	return resolveCatalogRefs(ctx, dto.CollectWalletRefs, entities...)
}
