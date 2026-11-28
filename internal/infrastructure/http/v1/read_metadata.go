package v1

import (
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
	"github.com/alex-bogatiuk/metapus/internal/platform"
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
		def.RLSDimensions = maps.Clone(identity.RLSDimensions)
		metadata.MustBindParts(&def, identity)
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
	if inspector, ok := factory.(platform.Inspectable); ok && len(def.RLSDimensions) > 0 {
		mustValidateRLSDimensions(factory.EntityName(), inspector.EntityStruct(), def.RLSDimensions)
	}
	if sources, ok := factory.(platform.MovementSourceProvider); ok {
		def.MovementSources = sources.MovementSources()
	}
	return def
}

// mustValidateRLSDimensions verifies at registration time that every RLS DB
// column declared by an entity actually exists on its Go model's db tags.
func mustValidateRLSDimensions(entityName string, sample any, dims map[string]string) {
	if sample == nil || len(dims) == 0 {
		return
	}
	cols := map[string]bool{}
	collectStructDBColumns(reflect.TypeOf(sample), cols)
	for dim, col := range dims {
		if !cols[col] {
			panic(fmt.Sprintf("entity %q: RLS dimension %q references unknown db column %q", entityName, dim, col))
		}
	}
}

func collectStructDBColumns(t reflect.Type, out map[string]bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			collectStructDBColumns(f.Type, out)
			continue
		}
		tag := strings.Split(f.Tag.Get("db"), ",")[0]
		if tag != "" && tag != "-" {
			out[tag] = true
		}
	}
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
