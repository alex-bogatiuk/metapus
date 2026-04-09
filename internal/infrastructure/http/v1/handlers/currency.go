package handlers

import (
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/infrastructure/http/v1/dto"
)

// CurrencyHTTPHandler is a type alias to keep signatures concise
type CurrencyHTTPHandler = CatalogHandler[
	*currency.Currency,
	dto.CreateCurrencyRequest,
	dto.UpdateCurrencyRequest,
]

// NewCurrencyHandler is a factory that creates a configured Generic Handler
func NewCurrencyHandler(
	base *BaseHandler,
	service *currency.Service,
) *CurrencyHTTPHandler {

	config := CatalogHandlerConfig[
		*currency.Currency,
		dto.CreateCurrencyRequest,
		dto.UpdateCurrencyRequest,
	]{
		// Connect Generic Service
		Service:    service.CatalogService,
		EntityName: "currency",

		// Map Create DTO -> Entity
		MapCreateDTO: func(req dto.CreateCurrencyRequest) *currency.Currency {
			return req.ToEntity()
		},

		// Map Update DTO -> Entity
		MapUpdateDTO: func(req dto.UpdateCurrencyRequest, existing *currency.Currency) *currency.Currency {
			req.ApplyTo(existing)
			return existing
		},

		// Map Entity -> Response DTO
		MapToDTO: func(entity *currency.Currency) any {
			return dto.FromCurrency(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
