package content

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/application/exchangerateapp"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

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
