package platformcontent

import (
	"metapus/internal/domain"
	"metapus/internal/domain/integrations"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/metadata"
)

// registerProviderEnum lists the catalog's providers as the values of
// integrations.Provider fields in metadata.
func registerProviderEnum(catalog *integrations.Catalog) {
	values := make([]metadata.EnumValue, 0, len(catalog.Providers()))
	for _, provider := range catalog.Providers() {
		values = append(values, metadata.EnumValue{Value: string(provider.ID), Label: provider.Label})
	}
	metadata.RegisterEnum[integrations.Provider](values)
}

// IntegrationAccountRegistration is the integration accounts catalog. Its
// providers come from the application's catalog; the configuration's pool
// consumers check new keys of an account their pools use.
type IntegrationAccountRegistration struct {
	v1.CatalogRegistrationFor[integrations.Account, dto.CreateIntegrationAccountRequest, dto.UpdateIntegrationAccountRequest, dto.IntegrationAccountResponse]
	catalog   *integrations.Catalog
	consumers []integrations.PoolConsumer
}

func (*IntegrationAccountRegistration) ReferenceTypes() []string {
	return []string{"integration_account"}
}
func (*IntegrationAccountRegistration) EntityLabel() string {
	return "Аккаунты интеграций"
}
func (*IntegrationAccountRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Аккаунт интеграции", Plural: "Аккаунты интеграций", NewLabel: "Создать", Genitive: "интеграции"}
}
func (*IntegrationAccountRegistration) SensitiveMutationAction() string {
	return "system.integrations.write"
}
func (r *IntegrationAccountRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewIntegrationAccountRepo(r.consumers...)
	service := integrations.NewAccountService(repo, deps.Numerator, r.catalog)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service, integrations.AccountDefinition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[*integrations.Account, dto.CreateIntegrationAccountRequest, dto.UpdateIntegrationAccountRequest]{
		Service: service, EntityName: integrations.AccountDefinition.Key,
		MapCreateDTO: func(req dto.CreateIntegrationAccountRequest) *integrations.Account { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateIntegrationAccountRequest, existing *integrations.Account) *integrations.Account {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(a *integrations.Account) any { return dto.FromIntegrationAccount(a) },
	})
}

// IntegrationPoolRegistration is the integration pools catalog. A pool
// consumer of the configuration keeps a pool it uses from being deleted or
// given another provider.
type IntegrationPoolRegistration struct {
	v1.CatalogRegistrationFor[integrations.Pool, dto.CreateIntegrationPoolRequest, dto.UpdateIntegrationPoolRequest, dto.IntegrationPoolResponse]
	catalog   *integrations.Catalog
	consumers []integrations.PoolConsumer
}

func (*IntegrationPoolRegistration) ReferenceTypes() []string { return []string{"integration_pool"} }
func (*IntegrationPoolRegistration) EntityLabel() string      { return "Пулы аккаунтов" }
func (*IntegrationPoolRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Пул аккаунтов", Plural: "Пулы аккаунтов", NewLabel: "Создать", Genitive: "интеграции"}
}
func (*IntegrationPoolRegistration) SensitiveMutationAction() string {
	return "system.integrations.write"
}
func (r *IntegrationPoolRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewIntegrationPoolRepo(r.consumers...)
	service := integrations.NewPoolService(repo, deps.Numerator, r.catalog)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service, integrations.PoolDefinition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[*integrations.Pool, dto.CreateIntegrationPoolRequest, dto.UpdateIntegrationPoolRequest]{
		Service: service, EntityName: integrations.PoolDefinition.Key,
		MapCreateDTO: func(req dto.CreateIntegrationPoolRequest) *integrations.Pool { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateIntegrationPoolRequest, existing *integrations.Pool) *integrations.Pool {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(a *integrations.Pool) any { return dto.FromIntegrationPool(a) },
	})
}
