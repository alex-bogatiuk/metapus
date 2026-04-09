package handlers

import (
	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/infrastructure/http/v1/dto"
)

// CounterpartyHTTPHandler is a type alias to keep function signatures concise
type CounterpartyHTTPHandler = CatalogHandler[
	*counterparty.Counterparty,
	dto.CreateCounterpartyRequest,
	dto.UpdateCounterpartyRequest,
]

// NewCounterpartyHandler is a factory function.
// It hides the complexity of configuring the Generic handler from the Router.
func NewCounterpartyHandler(
	base *BaseHandler,
	service *counterparty.Service,
) *CounterpartyHTTPHandler {

	// Mapping configuration lives here — alongside the entity and DTO, where it belongs.
	config := CatalogHandlerConfig[
		*counterparty.Counterparty,
		dto.CreateCounterpartyRequest,
		dto.UpdateCounterpartyRequest,
	]{
		// Pass the generic part of the service
		Service:    service.CatalogService,
		EntityName: "counterparty",

		// Map Create DTO -> Entity
		MapCreateDTO: func(req dto.CreateCounterpartyRequest) *counterparty.Counterparty {
			return req.ToEntity()
		},

		// Map Update DTO -> Entity
		MapUpdateDTO: func(req dto.UpdateCounterpartyRequest, existing *counterparty.Counterparty) *counterparty.Counterparty {
			req.ApplyTo(existing)
			return existing
		},

		// Map Entity -> Response DTO
		MapToDTO: func(entity *counterparty.Counterparty) any {
			return dto.FromCounterparty(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
