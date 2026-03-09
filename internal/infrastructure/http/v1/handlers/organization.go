package handlers

import (
	"metapus/internal/domain/catalogs/organization"
	"metapus/internal/infrastructure/http/v1/dto"
)

// OrganizationHandler handles HTTP requests for Organizations.
type OrganizationHandler = CatalogHandler[
	*organization.Organization,
	dto.CreateOrganizationRequest,
	dto.UpdateOrganizationRequest,
]

// NewOrganizationHandler creates a new OrganizationHandler.
func NewOrganizationHandler(base *BaseHandler, service *organization.Service) *OrganizationHandler {
	config := CatalogHandlerConfig[
		*organization.Organization,
		dto.CreateOrganizationRequest,
		dto.UpdateOrganizationRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "organization",

		// Map Create Request
		MapCreateDTO: func(req dto.CreateOrganizationRequest) *organization.Organization {
			return req.ToEntity()
		},

		// Map Update Request
		MapUpdateDTO: func(req dto.UpdateOrganizationRequest, existing *organization.Organization) *organization.Organization {
			req.ApplyTo(existing)
			return existing
		},

		// Map Response
		MapToDTO: func(entity *organization.Organization) any {
			return dto.FromOrganization(entity)
		},
	}

	return NewCatalogHandler(base, config)
}
