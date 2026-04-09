package handlers

import (
	"metapus/internal/domain/catalogs/unit"
	"metapus/internal/infrastructure/http/v1/dto"
)

// UnitHTTPHandler is a type alias for brevity
type UnitHTTPHandler = CatalogHandler[
	*unit.Unit,
	dto.CreateUnitRequest,
	dto.UpdateUnitRequest,
]

// NewUnitHandler is a factory that creates a configured Generic Handler
func NewUnitHandler(
	base *BaseHandler,
	service *unit.Service,
) *UnitHTTPHandler {

	config := CatalogHandlerConfig[
		*unit.Unit,
		dto.CreateUnitRequest,
		dto.UpdateUnitRequest,
	]{
		// Connect Generic Service
		Service:    service.CatalogService,
		EntityName: "unit",

		// Map Create DTO -> Entity
		MapCreateDTO: func(req dto.CreateUnitRequest) *unit.Unit {
			return req.ToEntity()
		},

		// Map Update DTO -> Entity
		MapUpdateDTO: func(req dto.UpdateUnitRequest, existing *unit.Unit) *unit.Unit {
			req.ApplyTo(existing)
			return existing
		},

		// Map Entity -> Response DTO
		MapToDTO: func(entity *unit.Unit) any {
			return dto.FromUnit(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
