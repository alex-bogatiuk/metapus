package a

import (
	"metapus/internal/domain"

	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/metadata"
)

type ARegistration struct {
	v1.CatalogRegistrationFor[A, CreateARequest, UpdateARequest, AResponse]
}

func (r *ARegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "A", Plural: "As", NewLabel: "New A"}
}

func (r *ARegistration) ReferenceTypes() []string { return []string{Definition.Key} }

func (r *ARegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	service := NewService(NewRepo(), deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)

	domain.NewEventLogCatalogService(service.CatalogService, Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[*A, CreateARequest, UpdateARequest]{
		Service: service.CatalogService, EntityName: Definition.Key,
		MapCreateDTO: func(req CreateARequest) *A { return req.ToEntity() },
		MapUpdateDTO: func(req UpdateARequest, e *A) *A { req.ApplyTo(e); return e },
		MapToDTO:     func(e *A) any { return FromA(e) },
	})
}
func Register(reg *v1.FactoryRegistry) { reg.RegisterCatalog(&ARegistration{}) }
