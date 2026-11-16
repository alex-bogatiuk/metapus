package content

import (
	"metapus/internal/domain"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres/crypto_integration_repo"
	"metapus/internal/metadata"
)

func init() {
	values := make([]metadata.EnumValue, 0)
	for _, provider := range cryptointegrations.Catalog().Providers() {
		values = append(values, metadata.EnumValue{Value: string(provider.ID), Label: provider.Label})
	}
	metadata.RegisterEnum[integrations.Provider](values)
}

type IntegrationAccountRegistration struct {
	v1.CatalogRegistrationFor[integrations.Account, dto.CreateIntegrationAccountRequest, dto.UpdateIntegrationAccountRequest, dto.IntegrationAccountResponse]
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
func (*IntegrationAccountRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := crypto_integration_repo.NewIntegrationAccountRepo()
	service := integrations.NewAccountService(repo, deps.Numerator, cryptointegrations.Catalog())
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

type IntegrationPoolRegistration struct {
	v1.CatalogRegistrationFor[integrations.Pool, dto.CreateIntegrationPoolRequest, dto.UpdateIntegrationPoolRequest, dto.IntegrationPoolResponse]
}

func (*IntegrationPoolRegistration) ReferenceTypes() []string { return []string{"integration_pool"} }
func (*IntegrationPoolRegistration) EntityLabel() string      { return "Пулы аккаунтов" }
func (*IntegrationPoolRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Пул аккаунтов", Plural: "Пулы аккаунтов", NewLabel: "Создать", Genitive: "интеграции"}
}
func (*IntegrationPoolRegistration) SensitiveMutationAction() string {
	return "system.integrations.write"
}
func (*IntegrationPoolRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := crypto_integration_repo.NewIntegrationPoolRepo()
	service := integrations.NewPoolService(repo, deps.Numerator, cryptointegrations.Catalog())
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

type CryptoRPCEndpointRegistration struct {
	v1.CatalogRegistrationFor[rpcconfig.RPCEndpoint, cryptodto.CreateCryptoRPCEndpointRequest, cryptodto.UpdateCryptoRPCEndpointRequest, cryptodto.CryptoRPCEndpointResponse]
}

func (*CryptoRPCEndpointRegistration) ReferenceTypes() []string {
	return []string{"crypto_rpc_endpoint"}
}
func (*CryptoRPCEndpointRegistration) EntityLabel() string { return "Подключения RPC" }
func (*CryptoRPCEndpointRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Подключение RPC", Plural: "Подключения RPC", NewLabel: "Новое подключение", Genitive: "подключения RPC"}
}
func (*CryptoRPCEndpointRegistration) SensitiveMutationAction() string {
	return "system.integrations.write"
}
func (*CryptoRPCEndpointRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := crypto_integration_repo.NewCryptoRPCEndpointRepo()
	service := rpcconfig.NewRPCEndpointService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service, rpcconfig.RPCEndpointDefinition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[*rpcconfig.RPCEndpoint, cryptodto.CreateCryptoRPCEndpointRequest, cryptodto.UpdateCryptoRPCEndpointRequest]{Service: service, EntityName: rpcconfig.RPCEndpointDefinition.Key, MapCreateDTO: func(r cryptodto.CreateCryptoRPCEndpointRequest) *rpcconfig.RPCEndpoint { return r.ToEntity() }, MapUpdateDTO: func(r cryptodto.UpdateCryptoRPCEndpointRequest, e *rpcconfig.RPCEndpoint) *rpcconfig.RPCEndpoint {
		r.Apply(e)
		return e
	}, MapToDTO: func(e *rpcconfig.RPCEndpoint) any { return cryptodto.CryptoRPCEndpointResponse{RPCEndpoint: *e} }})
}
