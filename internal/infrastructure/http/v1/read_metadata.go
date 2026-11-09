package v1

import (
	"metapus/internal/core/entity"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

type ReadMetadataRegistration interface {
	EntityName() string
	Permission() string
	RoutePrefix() string
}

// InspectReadMetadata is shared by HTTP and scheduled reports. In particular,
// permission and RLS dimensions always come from the same factory registration.
func InspectReadMetadata(factory ReadMetadataRegistration, kind metadata.EntityType) metadata.EntityDef {
	def := metadata.EntityDef{Name: factory.EntityName(), Type: kind}
	if inspector, ok := factory.(platform.Inspectable); ok {
		def = metadata.Inspect(inspector.EntityStruct(), factory.EntityName(), kind)
	}
	if provider, ok := factory.(entity.DefinitionProvider); ok {
		identity := provider.EntityDefinition()
		def.Key = identity.Key
		def.ReadPermission = identity.Permission() + ":read"
		def.RoutePrefix = identity.Route
		def.TableName = identity.Table
	} else if kind == metadata.TypeRegister {
		def.Key = deriveEntityKey(factory.Permission())
		def.ReadPermission = factory.Permission() + ":read"
		def.RoutePrefix = factory.RoutePrefix()
		if table, ok := factory.(platform.TableNameProvider); ok {
			def.TableName = table.TableName()
		}
	} else {
		panic("readable entity requires a definition: " + factory.EntityName())
	}

	if rls, ok := factory.(platform.RLSProvider); ok {
		def.RLSDimensions = rls.RLSDimensions()
	}
	return def
}

func BuildReadRegistry(factories *FactoryRegistry) *metadata.Registry {
	registry := metadata.NewRegistry()
	register := func(factory ReadMetadataRegistration, kind metadata.EntityType) {
		registry.Register(InspectReadMetadata(factory, kind))
		if refs, ok := factory.(platform.ReferenceProvider); ok {
			for _, ref := range refs.ReferenceTypes() {
				registry.RegisterReferenceMapping(ref, factory.EntityName())
			}
		}
	}
	for _, factory := range factories.Catalogs() {
		register(factory, metadata.TypeCatalog)
	}
	for _, factory := range factories.Documents() {
		register(factory, metadata.TypeDocument)
	}
	for _, factory := range factories.CustomDocuments() {
		register(factory, metadata.TypeDocument)
	}
	for _, factory := range factories.Registers() {
		if readable, ok := factory.(ReadMetadataRegistration); ok {
			register(readable, metadata.TypeRegister)
		}
	}
	return registry
}
