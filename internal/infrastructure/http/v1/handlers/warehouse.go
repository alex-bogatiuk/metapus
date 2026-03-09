package handlers

import (
	"metapus/internal/domain/catalogs/warehouse"
	"metapus/internal/infrastructure/http/v1/dto"
)

// WarehouseHTTPHandler - псевдоним
type WarehouseHTTPHandler = CatalogHandler[
	*warehouse.Warehouse,
	dto.CreateWarehouseRequest,
	dto.UpdateWarehouseRequest,
]

// NewWarehouseHandler - фабрика конфигурации
func NewWarehouseHandler(
	base *BaseHandler,
	service *warehouse.Service,
) *WarehouseHTTPHandler {

	config := CatalogHandlerConfig[
		*warehouse.Warehouse,
		dto.CreateWarehouseRequest,
		dto.UpdateWarehouseRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "warehouse",

		// Map Create Request
		MapCreateDTO: func(req dto.CreateWarehouseRequest) *warehouse.Warehouse {
			return req.ToEntity()
		},

		// Map Update Request
		MapUpdateDTO: func(req dto.UpdateWarehouseRequest, existing *warehouse.Warehouse) *warehouse.Warehouse {
			req.ApplyTo(existing)
			return existing
		},

		// Map Response
		MapToDTO: func(entity *warehouse.Warehouse) any {
			return dto.FromWarehouse(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
