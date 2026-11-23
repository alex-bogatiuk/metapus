package content

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_integration_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/handlers"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/v1"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/metadata"
)

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
