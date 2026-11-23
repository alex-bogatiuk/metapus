package platformcontent

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/contract"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/counterparty"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/currency"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/organization"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/rate_source"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
	"github.com/alex-bogatiuk/metapus/internal/platform"
)

func init() {
	// Register Enum metadata globally for automatic filter UI dropdown resolution.

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

	// Rate Source Types
	metadata.RegisterEnum[rate_source.SourceType]([]metadata.EnumValue{
		{Value: "coingecko", Label: "CoinGecko"},
		{Value: "binance", Label: "Binance"},
		{Value: "coinmarketcap", Label: "CoinMarketCap"},
		{Value: "manual", Label: "Ручной ввод"},
	})
}

// ---------------------------------------------------------------------------
// Shared catalogs declared from their models: DTOs, routes, contracts,
// references and permissions are derived; each keeps its domain service.
// ---------------------------------------------------------------------------

func counterpartyRegistration() v1.CatalogRegistration {
	return v1.Catalog[counterparty.Counterparty](metadata.Presentation{
		Singular: "Контрагент", Plural: "Контрагенты", NewLabel: "Новый контрагент", Genitive: "контрагента",
	}).
		References("supplier", "customer").
		Search(platform.SearchFields{SearchCols: []string{"name", "code", "inn", "phone", "email"}, TitleCol: "name", SubtitleCol: "code"}).
		Service(func(deps v1.CatalogDeps) *domain.CatalogService[*counterparty.Counterparty] {
			return counterparty.NewService(catalog_repo.NewCounterpartyRepo(), deps.Numerator).CatalogService
		})
}

func currencyRegistration() v1.CatalogRegistration {
	return v1.Catalog[currency.Currency](metadata.Presentation{
		Singular: "Валюта", Plural: "Валюты", NewLabel: "Новая валюта", Genitive: "валюты",
	}).
		Service(func(deps v1.CatalogDeps) *domain.CatalogService[*currency.Currency] {
			return currency.NewService(catalog_repo.NewCurrencyRepo(), deps.Numerator).CatalogService
		})
}

func organizationRegistration() v1.CatalogRegistration {
	return v1.Catalog[organization.Organization](metadata.Presentation{
		Singular: "Организация", Plural: "Организации", NewLabel: "Новая организация", Genitive: "организации",
	}).
		Service(func(deps v1.CatalogDeps) *domain.CatalogService[*organization.Organization] {
			service := organization.NewService(catalog_repo.NewOrganizationRepo(), deps.Numerator)
			// Invalidate CurrencyResolver cache when org's base currency changes
			if inv := deps.CurrencyCacheInvalidator; inv != nil {
				service.Hooks().OnAfterUpdate(func(_ context.Context, org *organization.Organization) error {
					inv.InvalidateOrgCurrency(org.ID)
					return nil
				})
			}
			return service.CatalogService
		})
}

func contractRegistration() v1.CatalogRegistration {
	return v1.Catalog[contract.Contract](metadata.Presentation{
		Singular: "Договор", Plural: "Договоры", NewLabel: "Новый договор", Genitive: "договора",
	}).
		Service(func(deps v1.CatalogDeps) *domain.CatalogService[*contract.Contract] {
			service := contract.NewService(catalog_repo.NewContractRepo(), deps.Numerator)
			// Invalidate CurrencyResolver cache when contract's currency changes
			if inv := deps.CurrencyCacheInvalidator; inv != nil {
				service.Hooks().OnAfterUpdate(func(_ context.Context, c *contract.Contract) error {
					inv.InvalidateContractCurrency(c.ID)
					return nil
				})
			}
			return service.CatalogService
		})
}
