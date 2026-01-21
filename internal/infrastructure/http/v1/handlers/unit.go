package handlers

import (
	"metapus/internal/domain/catalogs/unit"
	"metapus/internal/infrastructure/http/v1/dto"
)

// UnitHTTPHandler - псевдоним типа для сокращения
type UnitHTTPHandler = CatalogHandler[
	*unit.Unit,
	dto.CreateUnitRequest,
	dto.UpdateUnitRequest,
]

// NewUnitHandler - фабрика для создания настроенного Generic Handler
func NewUnitHandler(
	base *BaseHandler,
	service *unit.Service,
) *UnitHTTPHandler {

	config := CatalogHandlerConfig[
		*unit.Unit,
		dto.CreateUnitRequest,
		dto.UpdateUnitRequest,
	]{
		// Подключаем Generic Service
		Service:    service.CatalogService,
		EntityName: "unit",

		// Маппинг: DTO создания -> Сущность
		MapCreateDTO: func(req dto.CreateUnitRequest) *unit.Unit {
			return req.ToEntity()
		},

		// Маппинг: DTO обновления -> Сущность
		MapUpdateDTO: func(req dto.UpdateUnitRequest, existing *unit.Unit) *unit.Unit {
			req.ApplyTo(existing)
			return existing
		},

		// Маппинг: Сущность -> DTO ответа
		MapToDTO: func(entity *unit.Unit) any {
			return dto.FromUnit(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
