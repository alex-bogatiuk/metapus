package handlers

import (
	"metapus/internal/domain/catalogs/nomenclature"
	"metapus/internal/infrastructure/http/v1/dto"
)

// NomenclatureHTTPHandler is a type alias for code brevity
type NomenclatureHTTPHandler = CatalogHandler[
	*nomenclature.Nomenclature,
	dto.CreateNomenclatureRequest,
	dto.UpdateNomenclatureRequest,
]

// NewNomenclatureHandler is a factory function that hides generic configuration complexity
func NewNomenclatureHandler(
	base *BaseHandler,
	service *nomenclature.Service,
) *NomenclatureHTTPHandler {

	config := CatalogHandlerConfig[
		*nomenclature.Nomenclature,
		dto.CreateNomenclatureRequest,
		dto.UpdateNomenclatureRequest,
	]{
		// Use the generic service embedded within the Nomenclature service
		Service:    service.CatalogService,
		EntityName: "nomenclature",

		// Map Request DTO -> Entity
		MapCreateDTO: func(req dto.CreateNomenclatureRequest) *nomenclature.Nomenclature {
			return req.ToEntity()
		},

		// Map Update DTO -> Entity
		MapUpdateDTO: func(req dto.UpdateNomenclatureRequest, existing *nomenclature.Nomenclature) *nomenclature.Nomenclature {
			req.ApplyTo(existing)
			return existing
		},

		// Map Entity -> Response DTO
		MapToDTO: func(entity *nomenclature.Nomenclature) any {
			return dto.FromNomenclature(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
