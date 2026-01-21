package handlers

import (
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/infrastructure/http/v1/dto"
)

// CurrencyHTTPHandler - псевдоним типа для сокращения сигнатур
type CurrencyHTTPHandler = CatalogHandler[
	*currency.Currency,
	dto.CreateCurrencyRequest,
	dto.UpdateCurrencyRequest,
]

// NewCurrencyHandler - фабрика, создающая настроенный Generic Handler
func NewCurrencyHandler(
	base *BaseHandler,
	service *currency.Service,
) *CurrencyHTTPHandler {

	config := CatalogHandlerConfig[
		*currency.Currency,
		dto.CreateCurrencyRequest,
		dto.UpdateCurrencyRequest,
	]{
		// Подключаем Generic Service
		Service:    service.CatalogService,
		EntityName: "currency",

		// Маппинг: DTO создания -> Сущность
		MapCreateDTO: func(req dto.CreateCurrencyRequest) *currency.Currency {
			return req.ToEntity()
		},

		// Маппинг: DTO обновления -> Сущность
		MapUpdateDTO: func(req dto.UpdateCurrencyRequest, existing *currency.Currency) *currency.Currency {
			req.ApplyTo(existing)
			return existing
		},

		// Маппинг: Сущность -> DTO ответа
		MapToDTO: func(entity *currency.Currency) any {
			return dto.FromCurrency(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
