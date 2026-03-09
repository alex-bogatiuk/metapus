package handlers

import (
	"metapus/internal/domain/catalogs/nomenclature"
	"metapus/internal/infrastructure/http/v1/dto"
)

// NomenclatureHTTPHandler - псевдоним для сокращения кода
type NomenclatureHTTPHandler = CatalogHandler[
	*nomenclature.Nomenclature,
	dto.CreateNomenclatureRequest,
	dto.UpdateNomenclatureRequest,
]

// NewNomenclatureHandler - фабрика, скрывающая сложность Generic-конфигурации
func NewNomenclatureHandler(
	base *BaseHandler,
	service *nomenclature.Service,
) *NomenclatureHTTPHandler {

	config := CatalogHandlerConfig[
		*nomenclature.Nomenclature,
		dto.CreateNomenclatureRequest,
		dto.UpdateNomenclatureRequest,
	]{
		// Используем Generic Service, встроенный в Nomenclature Service
		Service:    service.CatalogService,
		EntityName: "nomenclature",

		// Маппинг Request DTO -> Entity
		MapCreateDTO: func(req dto.CreateNomenclatureRequest) *nomenclature.Nomenclature {
			return req.ToEntity()
		},

		// Маппинг Update DTO -> Entity
		MapUpdateDTO: func(req dto.UpdateNomenclatureRequest, existing *nomenclature.Nomenclature) *nomenclature.Nomenclature {
			req.ApplyTo(existing)
			return existing
		},

		// Маппинг Entity -> Response DTO
		MapToDTO: func(entity *nomenclature.Nomenclature) any {
			return dto.FromNomenclature(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
