package handlers

import (
	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/infrastructure/http/v1/dto"
)

// Создаем алиас типа, чтобы сигнатуры функций не были километровыми
type CounterpartyHTTPHandler = CatalogHandler[
	*counterparty.Counterparty,
	dto.CreateCounterpartyRequest,
	dto.UpdateCounterpartyRequest,
]

// NewCounterpartyHandler - это Фабрика.
// Она скрывает сложность настройки Generic-хендлера от Роутера.
func NewCounterpartyHandler(
	base *BaseHandler,
	service *counterparty.Service,
) *CounterpartyHTTPHandler {

	// Конфигурация маппинга живет здесь — рядом с сущностью и DTO, где ей и место.
	config := CatalogHandlerConfig[
		*counterparty.Counterparty,
		dto.CreateCounterpartyRequest,
		dto.UpdateCounterpartyRequest,
	]{
		// Передаем Generic-часть сервиса
		Service:    service.CatalogService,
		EntityName: "counterparty",

		// Маппинг Create DTO -> Entity
		MapCreateDTO: func(req dto.CreateCounterpartyRequest) *counterparty.Counterparty {
			return req.ToEntity()
		},

		// Маппинг Update DTO -> Entity
		MapUpdateDTO: func(req dto.UpdateCounterpartyRequest, existing *counterparty.Counterparty) *counterparty.Counterparty {
			req.ApplyTo(existing)
			return existing
		},

		// Маппинг Entity -> Response DTO
		MapToDTO: func(entity *counterparty.Counterparty) any {
			return dto.FromCounterparty(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
