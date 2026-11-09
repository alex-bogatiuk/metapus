package tree_probe

import (
	"metapus/internal/domain"

	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/metadata"
)

type TreeProbeRegistration struct {
	v1.CatalogRegistrationFor[TreeProbe, CreateTreeProbeRequest, UpdateTreeProbeRequest, TreeProbeResponse]
}

func (r *TreeProbeRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "TreeProbe", Plural: "TreeProbes", NewLabel: "New TreeProbe"}
}

func (r *TreeProbeRegistration) ReferenceTypes() []string { return []string{Definition.Key} }

func (r *TreeProbeRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	service := NewService(NewRepo(), deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)

	domain.NewEventLogCatalogService(service.CatalogService, Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[*TreeProbe, CreateTreeProbeRequest, UpdateTreeProbeRequest]{
		Service: service.CatalogService, EntityName: Definition.Key,
		MapCreateDTO: func(req CreateTreeProbeRequest) *TreeProbe { return req.ToEntity() },
		MapUpdateDTO: func(req UpdateTreeProbeRequest, e *TreeProbe) *TreeProbe { req.ApplyTo(e); return e },
		MapToDTO:     func(e *TreeProbe) any { return FromTreeProbe(e) },
	})
}
func Register(reg *v1.FactoryRegistry) { reg.RegisterCatalog(&TreeProbeRegistration{}) }
