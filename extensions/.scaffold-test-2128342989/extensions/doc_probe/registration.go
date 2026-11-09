package doc_probe

import (
	"metapus/internal/domain"
	"metapus/internal/infrastructure/http/contracts/spec"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/metadata"
)

type DocProbeRegistration struct {
	v1.EntityRegistration[DocProbe]
}

func (r *DocProbeRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "DocProbe", Plural: "DocProbes", NewLabel: "New DocProbe"}
}

func (r *DocProbeRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{List: true, Read: true, Create: true, Update: true, Delete: true, SetDeletionMark: true, Post: true, Unpost: true, Repost: true, ExportList: true, Movements: true, BatchAction: true, BatchActionByFilter: true}
}

func (r *DocProbeRegistration) HTTPContracts() spec.Bundle {
	return spec.Entity[CreateDocProbeRequest, UpdateDocProbeRequest, DocProbeResponse](r.EntityDefinition(), r.DocumentCapabilities())
}

func (r *DocProbeRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	service := NewService(NewRepo(), deps.Numerator, deps.PostingEngine)
	service.SetPolicyEngine(deps.PolicyEngine)

	decorated := domain.WithEventLog[*DocProbe](Definition.Key, deps.EventWriter)(service)
	return handlers.NewBaseDocumentHandler(deps.BaseHandler, handlers.BaseDocumentHandlerConfig[*DocProbe, CreateDocProbeRequest, UpdateDocProbeRequest]{
		Service: decorated, EntityName: Definition.Key,
		MapCreateDTO:      func(req CreateDocProbeRequest) (*DocProbe, error) { return req.ToEntity(), nil },
		MapUpdateDTO:      func(req UpdateDocProbeRequest, e *DocProbe) (*DocProbe, error) { req.ApplyTo(e); return e, nil },
		MapToDTO:          func(e *DocProbe) any { return FromDocProbe(e) },
		IsPostImmediately: func(req CreateDocProbeRequest) bool { return req.PostImmediately },
		MovementProviders: deps.MovementProviders, MovementRefResolver: deps.MovementRefResolver, SettingsRepo: deps.SettingsRepo,
	})

}
func Register(reg *v1.FactoryRegistry) { reg.RegisterDocument(&DocProbeRegistration{}) }
