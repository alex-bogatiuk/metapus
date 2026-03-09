package handlers

import (
	"metapus/internal/domain/catalogs/vat_rate"
	"metapus/internal/infrastructure/http/v1/dto"
)

// VATRateHTTPHandler - псевдоним типа для сокращения
type VATRateHTTPHandler = CatalogHandler[
	*vat_rate.VATRate,
	dto.CreateVATRateRequest,
	dto.UpdateVATRateRequest,
]

// NewVATRateHandler - фабрика для создания настроенного Generic Handler
func NewVATRateHandler(
	base *BaseHandler,
	service *vat_rate.Service,
) *VATRateHTTPHandler {

	config := CatalogHandlerConfig[
		*vat_rate.VATRate,
		dto.CreateVATRateRequest,
		dto.UpdateVATRateRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "vat_rate",

		MapCreateDTO: func(req dto.CreateVATRateRequest) *vat_rate.VATRate {
			return req.ToEntity()
		},

		MapUpdateDTO: func(req dto.UpdateVATRateRequest, existing *vat_rate.VATRate) *vat_rate.VATRate {
			req.ApplyTo(existing)
			return existing
		},

		MapToDTO: func(entity *vat_rate.VATRate) any {
			return dto.FromVATRate(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
