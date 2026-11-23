package platformcontent

import (
	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/application/exchangerateapp"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/rate_source"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts/spec"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/handlers"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/register_repo"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

// ---------------------------------------------------------------------------
// RateSource
// ---------------------------------------------------------------------------

type RateSourceRegistration struct {
	v1.CatalogRegistrationFor[rate_source.RateSource, dto.CreateRateSourceRequest, dto.UpdateRateSourceRequest, dto.RateSourceResponse]
}

func (r *RateSourceRegistration) ReferenceTypes() []string { return []string{"rate_source"} }
func (r *RateSourceRegistration) EntityLabel() string      { return "Источники курсов" }
func (r *RateSourceRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Источник курсов",
		Plural:   "Источники курсов",
		NewLabel: "Новый источник курсов",
		Genitive: "источника курсов",
	}
}
func (r *RateSourceRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewRateSourceRepo()
	service := rate_source.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, rate_source.Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*rate_source.RateSource,
		dto.CreateRateSourceRequest,
		dto.UpdateRateSourceRequest,
	]{
		Service:      service.CatalogService,
		EntityName:   rate_source.Definition.Key,
		MapCreateDTO: func(req dto.CreateRateSourceRequest) *rate_source.RateSource { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateRateSourceRequest, existing *rate_source.RateSource) *rate_source.RateSource {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *rate_source.RateSource) any { return dto.FromRateSource(entity) },
	})
}

func (r *RateSourceRegistration) HTTPContracts() spec.Bundle {
	b := r.CatalogRegistrationFor.HTTPContracts()
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/mappings", "", "ExchangeRateMappingsResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/feed-state", "", "ExchangeRateFeedStateResponse")
	b.AddRoute(r.EntityDefinition(), "PUT", "/:id/mappings/:currencyId/:baseCurrencyId", "UpdateRateMappingRequest", "ExchangeRateMappingResponse")

	return b
}

func exchangeRatesHandler() *handlers.ExchangeRatesHandler {
	return handlers.NewExchangeRatesHandler(exchangerateapp.NewRegisterService(register_repo.NewExchangeRateRepo(), register_repo.RateSnapshot))
}

type ExchangeRateRegisterRegistration struct{}

func (*ExchangeRateRegisterRegistration) RoutePrefix() string { return "exchange-rates" }
func (*ExchangeRateRegisterRegistration) RegisterRoutes(group *gin.RouterGroup, _ v1.RouterConfig) {
	h := exchangeRatesHandler()
	group.GET("/current", middleware.RequirePermission("register:exchange_rate:read"), h.Current)
	group.GET("/history", middleware.RequirePermission("register:exchange_rate:read"), h.History)
	group.GET("/:id", middleware.RequirePermission("register:exchange_rate:read"), h.Quote)
}
func (*RateSourceRegistration) RegisterSubresourceRoutes(group *gin.RouterGroup, _ v1.RouterConfig) {
	h := exchangeRatesHandler()
	group.GET("/:id/mappings", middleware.RequirePermission("catalog:rate_source:read"), h.Mappings)
	group.GET("/:id/feed-state", middleware.RequirePermission("catalog:rate_source:read"), h.FeedState)
	group.PUT("/:id/mappings/:currencyId/:baseCurrencyId", middleware.RequireAllPermissions("catalog:rate_source:read", "catalog:rate_source:update"), h.PutMapping)
}
