package handlers

import (
	"metapus/internal/domain/catalogs/contract"
	"metapus/internal/infrastructure/http/v1/dto"
)

// ContractHTTPHandler is a type alias for brevity
type ContractHTTPHandler = CatalogHandler[
	*contract.Contract,
	dto.CreateContractRequest,
	dto.UpdateContractRequest,
]

// NewContractHandler is a factory that creates a configured Generic Handler
func NewContractHandler(
	base *BaseHandler,
	service *contract.Service,
) *ContractHTTPHandler {

	config := CatalogHandlerConfig[
		*contract.Contract,
		dto.CreateContractRequest,
		dto.UpdateContractRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "contract",

		MapCreateDTO: func(req dto.CreateContractRequest) *contract.Contract {
			return req.ToEntity()
		},

		MapUpdateDTO: func(req dto.UpdateContractRequest, existing *contract.Contract) *contract.Contract {
			req.ApplyTo(existing)
			return existing
		},

		MapToDTO: func(entity *contract.Contract) any {
			return dto.FromContract(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
