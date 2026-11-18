package v1

import (
	"context"
	"fmt"
	"reflect"

	"metapus/internal/core/entity"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

// Declarative registration: an applied module declares a model and registers
// it in one line; storage, service, CRUD routes, wire contracts, metadata and
// permissions are derived from the model and its Definition.
//
//	type Vehicle struct {
//		entity.Catalog
//		Plate string `db:"plate" json:"plate" meta:"label:Госномер" binding:"required"`
//	}
//	func (Vehicle) EntityDefinition() entity.Definition { return Definition }
//
//	reg.RegisterCatalog(v1.Catalog[Vehicle](metadata.Presentation{Singular: "ТС", Plural: "Транспорт"}))
//
// The wire projection follows one rule (see autoShape); business behaviour
// stays on the model (Validate, CanPost, movement sources) or in hooks added
// with Configure. An entity that needs its own DTOs or custom routes keeps an
// explicit registration (CatalogRegistrationFor).

// catalogModel is satisfied by *T for a catalog model T.
type catalogModel[T any] interface {
	*T
	entity.CatalogEntity
}

// documentModel is satisfied by *T for a document model T.
type documentModel[T any] interface {
	*T
	domain.HeaderDocumentEntity
}

// AutoCatalog is a catalog registration derived from its model.
type AutoCatalog[T entity.DefinitionProvider, PT catalogModel[T]] struct {
	EntityRegistration[T]
	autoRegistry
	presentation metadata.Presentation
	configure    []func(*domain.CatalogService[PT])
	service      func(CatalogDeps) *domain.CatalogService[PT]
	references   []string
	search       *platform.SearchFields
	responseName string
}

// Catalog declares a catalog from its model T, which embeds entity.Catalog
// and returns its Definition from EntityDefinition.
func Catalog[T entity.DefinitionProvider, PT catalogModel[T]](presentation metadata.Presentation) *AutoCatalog[T, PT] {
	r := &AutoCatalog[T, PT]{presentation: presentation}
	autoShapeFor[T]() // fail at registration, not on the first request
	return r
}

// Configure adds hooks or settings to the catalog service when it is built.
func (r *AutoCatalog[T, PT]) Configure(fn func(*domain.CatalogService[PT])) *AutoCatalog[T, PT] {
	r.configure = append(r.configure, fn)
	return r
}

// Service builds the catalog on the entity's own domain service (its
// repository, hooks and business rules) instead of the generic one; the
// registration still derives DTOs, routes, contracts, event log and policies.
func (r *AutoCatalog[T, PT]) Service(build func(CatalogDeps) *domain.CatalogService[PT]) *AutoCatalog[T, PT] {
	r.service = build
	return r
}

// References replaces the reference keys other models use for this catalog in
// meta:"ref:<key>"; by default the entity key.
func (r *AutoCatalog[T, PT]) References(keys ...string) *AutoCatalog[T, PT] {
	r.references = keys
	return r
}

// ResponseName names the response contract when the standard <Name>Response
// is taken by another contract; the list and tree contracts follow it.
func (r *AutoCatalog[T, PT]) ResponseName(name string) *AutoCatalog[T, PT] {
	r.responseName = name
	return r
}

// Search sets the columns the global search matches and shows; without it a
// catalog is searched by name and code.
func (r *AutoCatalog[T, PT]) Search(fields platform.SearchFields) *AutoCatalog[T, PT] {
	r.search = &fields
	return r
}

func (r *AutoCatalog[T, PT]) EntityPresentation() metadata.Presentation { return r.presentation }
func (r *AutoCatalog[T, PT]) EntityLabel() string                       { return r.presentation.Plural }
func (r *AutoCatalog[T, PT]) ReferenceTypes() []string {
	if len(r.references) > 0 {
		return r.references
	}
	return []string{r.EntityDefinition().Key}
}

// SearchableFields is declared only through Search; see autoSearchable.
func (r *AutoCatalog[T, PT]) searchFields() *platform.SearchFields { return r.search }

func (r *AutoCatalog[T, PT]) HTTPContracts() spec.Bundle {
	shape := autoShapeFor[T]()
	return spec.EntityOf(r.EntityDefinition(), metadata.DocumentCapabilities{}, shape.create, shape.update, shape.read, r.responseName)
}

func (r *AutoCatalog[T, PT]) Build(deps CatalogDeps) CatalogRouteHandler {
	def := r.EntityDefinition()
	shape := autoShapeFor[T]()
	var service *domain.CatalogService[PT]
	if r.service != nil {
		service = r.service(deps)
		service.SetPolicyEngine(deps.PolicyEngine)
	} else {
		repo := catalog_repo.NewCatalogRepoForDefinition(def, postgres.ExtractDBColumns[T](), func() PT { return new(T) })
		service = domain.NewCatalogService(domain.CatalogServiceConfig[PT]{Definition: &def, Repo: repo, Numerator: deps.Numerator, PolicyEngine: deps.PolicyEngine})
	}
	for _, fn := range r.configure {
		fn(service)
	}
	domain.NewEventLogCatalogService(service, def.Key, deps.EventWriter)
	tables := r.referenceTables(shape)
	resolveRefs, mapWithRefs := autoReferenceMappers[PT](shape, tables)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[PT, AutoCreate[T], AutoUpdate[T]]{
		Service:          service,
		EntityName:       def.Key,
		ResolveRefs:      resolveRefs,
		MapToDTOWithRefs: mapWithRefs,
		MapCreateDTO: func(req AutoCreate[T]) PT {
			model := newAutoModel[T](req.value, entity.CatalogKind)
			shape.applyAutoRequest(req.value, model)
			return model
		},
		MapUpdateDTO: func(req AutoUpdate[T], existing PT) PT {
			shape.applyAutoRequest(req.value, existing)
			reflect.ValueOf(existing).Elem().FieldByName("Version").SetInt(req.value.FieldByName("Version").Int())
			return existing
		},
		MapToDTO: func(model PT) any { return shape.response(model, tables, nil) },
	})
}

// autoDocumentCapabilities is the generic surface of a declared document.
var autoDocumentCapabilities = metadata.DocumentCapabilities{
	List: true, Read: true, Create: true, Update: true, Delete: true, SetDeletionMark: true,
	Post: true, Unpost: true, Repost: true, ExportList: true, Movements: true, BatchAction: true, BatchActionByFilter: true,
}

// AutoDocument is a document registration derived from its model: the header
// and the table parts its Definition declares.
type AutoDocument[T entity.DefinitionProvider, PT documentModel[T]] struct {
	EntityRegistration[T]
	autoRegistry
	presentation metadata.Presentation
	numberPrefix string
	configure    []func(*domain.BaseHeaderDocumentService[PT])
}

// Document declares a document from its model T, which embeds entity.Document
// and returns its Definition from EntityDefinition. Each table part in the
// Definition is a []Line field of T named by the part's Field; its rows are
// stored, validated and served with the header (see autoShape and
// document_repo.PartStore).
func Document[T entity.DefinitionProvider, PT documentModel[T]](presentation metadata.Presentation) *AutoDocument[T, PT] {
	r := &AutoDocument[T, PT]{presentation: presentation}
	autoShapeFor[T]()
	return r
}

// NumberPrefix sets the numerator prefix of document numbers; by default it
// is derived from the entity key.
func (r *AutoDocument[T, PT]) NumberPrefix(prefix string) *AutoDocument[T, PT] {
	r.numberPrefix = prefix
	return r
}

// Configure adds hooks or settings to the document service when it is built.
func (r *AutoDocument[T, PT]) Configure(fn func(*domain.BaseHeaderDocumentService[PT])) *AutoDocument[T, PT] {
	r.configure = append(r.configure, fn)
	return r
}

func (r *AutoDocument[T, PT]) EntityPresentation() metadata.Presentation { return r.presentation }
func (r *AutoDocument[T, PT]) DocumentCapabilities() metadata.DocumentCapabilities {
	return autoDocumentCapabilities
}

func (r *AutoDocument[T, PT]) HTTPContracts() spec.Bundle {
	def := r.EntityDefinition()
	shape := autoShapeFor[T]()
	bundle := spec.EntityOf(def, r.DocumentCapabilities(), shape.create, shape.update, shape.read, "")
	for _, part := range shape.parts {
		bundle.Contracts = append(bundle.Contracts,
			spec.Contract{Name: def.Name + part.contract + "LineRequest", Type: part.create, Direction: spec.Input},
			spec.Contract{Name: def.Name + part.contract + "LineResponse", Type: part.read, Direction: spec.Output})
	}
	return bundle
}

func (r *AutoDocument[T, PT]) Build(deps DocumentDeps) DocumentRouteHandler {
	def := r.EntityDefinition()
	shape := autoShapeFor[T]()
	prefix := r.numberPrefix
	if prefix == "" {
		prefix = entity.DefaultCodePrefix(def.Key)
	}
	repo := document_repo.NewDocumentRepoForDefinition(def, postgres.ExtractDBColumns[T](), func() PT { return new(T) })
	service := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[PT]{
		Definition: &def, Repo: repo, PostingEngine: deps.PostingEngine, Numerator: deps.Numerator, PolicyEngine: deps.PolicyEngine,
		NumeratorPrefix: prefix, NumeratorStrategy: numerator.StrategyStrict, EntityName: def.Key,
		Parts: autoDocumentParts(def, repo),
	})
	for _, fn := range r.configure {
		fn(service)
	}
	decorated := domain.WithEventLog[PT](def.Key, deps.EventWriter)(service)
	tables := r.referenceTables(shape)
	resolveRefs, mapWithRefs := autoReferenceMappers[PT](shape, tables)
	return handlers.NewBaseDocumentHandler(deps.BaseHandler, handlers.BaseDocumentHandlerConfig[PT, AutoCreate[T], AutoUpdate[T]]{
		Service:          decorated,
		EntityName:       def.Key,
		ResolveRefs:      resolveRefs,
		MapToDTOWithRefs: mapWithRefs,
		MapCreateDTO: func(req AutoCreate[T]) (PT, error) {
			model := newAutoModel[T](req.value, entity.DocumentKind)
			shape.applyAutoRequest(req.value, model)
			if err := shape.computeAutoCalcs(model); err != nil {
				return nil, err
			}
			return model, nil
		},
		MapUpdateDTO: func(req AutoUpdate[T], existing PT) (PT, error) {
			shape.applyAutoRequest(req.value, existing)
			reflect.ValueOf(existing).Elem().FieldByName("Version").SetInt(req.value.FieldByName("Version").Int())
			if err := shape.computeAutoCalcs(existing); err != nil {
				return nil, err
			}
			return existing, nil
		},
		MapToDTO:            func(model PT) any { return shape.response(model, tables, nil) },
		IsPostImmediately:   func(req AutoCreate[T]) bool { return req.value.FieldByName("PostImmediately").Bool() },
		MovementProviders:   deps.MovementProviders,
		MovementRefResolver: deps.MovementRefResolver,
		SettingsRepo:        deps.SettingsRepo,
	})
}

// autoDocumentParts is the store of the document's table parts, nil when the
// Definition declares none; list filters of repo reach the parts' lines.
func autoDocumentParts[T any, PT documentModel[T]](def entity.Definition, repo *document_repo.BaseDocumentRepo[PT]) domain.DocumentParts[PT] {
	if len(def.Parts) == 0 {
		return nil
	}
	store := document_repo.NewPartStore[PT](def)
	store.RegisterFilters(repo)
	return store
}

// autoRegistry gives a declared registration the registry it was added to, so
// Build can find the tables of the entities its references point to.
type autoRegistry struct{ registry *FactoryRegistry }

func (a *autoRegistry) bindRegistry(r *FactoryRegistry) { a.registry = r }

// referenceTables maps each reference key of the shape to the table of the
// registered catalog or document that answers to it (its entity key or one
// of its ReferenceTypes). An unknown key is a declaration error.
func (a *autoRegistry) referenceTables(shape *autoShape) map[string]string {
	tables := map[string]string{}
	refs := shape.allRefs()
	if len(refs) == 0 {
		return tables
	}
	known := map[string]string{}
	if a.registry != nil {
		add := func(reg any, def entity.Definition) {
			known[def.Key] = def.Table
			if provider, ok := reg.(platform.ReferenceProvider); ok {
				for _, key := range provider.ReferenceTypes() {
					if _, taken := known[key]; !taken {
						known[key] = def.Table
					}
				}
			}
		}
		for _, c := range a.registry.catalogs {
			add(c, c.EntityDefinition())
		}
		for _, d := range a.registry.documents {
			add(d, d.EntityDefinition())
		}
	}
	for _, ref := range refs {
		table, ok := known[ref.key]
		if !ok {
			panic(fmt.Sprintf("declared reference %s → %q: no registered entity answers to this key", ref.field, ref.key))
		}
		tables[ref.key] = table
	}
	return tables
}

// autoReferenceMappers resolves the shape's references for a page of models
// through the authorized reference reader; nil when it has none.
func autoReferenceMappers[PT any](shape *autoShape, tables map[string]string) (func(context.Context, ...PT) (any, error), func(PT, any) any) {
	if len(shape.allRefs()) == 0 {
		return nil, nil
	}
	resolve := func(ctx context.Context, models ...PT) (any, error) {
		resolver := postgres.NewReferenceResolver()
		for _, model := range models {
			shape.collectRefs(resolver, tables, model)
		}
		return resolver.Resolve(ctx, nil)
	}
	mapWithRefs := func(model PT, refs any) any {
		resolved, _ := refs.(postgres.ResolvedRefs)
		return shape.response(model, tables, resolved)
	}
	return resolve, mapWithRefs
}

// autoSearchable is implemented by declared catalogs that set Search.
type autoSearchable interface{ searchFields() *platform.SearchFields }
